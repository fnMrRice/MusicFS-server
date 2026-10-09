# MusicFS Server 内部架构设计

状态：长期架构与实现约束；2026-10-09。最小本地播放已有实现，具体范围见[实施记录](validation/progress.md)，本文全部设计不因此视为已完成。跨端行为、公共 API 与事件契约以[总体架构](../../docs/architecture.md)为准；Server 在本仓库 `docs/contracts` 维护 OpenAPI 与 JSON Schema，不在内部设计中另立一套接口。

## 1. 范围与模块

Server 管理音乐来源、统一音乐库、歌单、信息刮削和音频流交付。Music Provider、Scraper、Audio Processor 是独立插件角色。本地文件与已挂载 NAS 属于首版正式范围，不依赖在线音乐账号。网易云、QQ 音乐是目标来源，接入、登录、推荐歌单及播放能力须分别验证，不承诺全量支持。

| 模块 | 内部职责 |
| --- | --- |
| API Gateway | 鉴权、校验、游标分页、幂等、冲突检测及统一错误 |
| Library | 规范化元数据、录音版本、来源/资产、匹配证据、人工关联和禁止关联 |
| Playlist | 本地歌单、远程快照、聚合规则、出处与隐藏规则 |
| Metadata Enrichment | Scraper 调度、候选预览/选用、人工锁定和原文件写入任务 |
| Resolver | 按权限、可用性、音质、来源偏好和处理能力选择来源与输出 |
| Plugin Host | 生命周期、角色注册、实例配置、权限、版本协商、限流、超时和取消 |
| Audio Pipeline | 解复用、解码、PCM DSP、编码与封装，见[音频处理](audio-processing.md) |
| Stream Gateway | 会话鉴权、原始字节/处理输出交付，以及真实的 Range/时间定位 |
| Scheduler | 扫描、探测、摘要、指纹、同步、刮削、写回和恢复 |
| Storage | SQLite WAL、单写入队列、Media Store、可重建缓存和独立密钥保护 |

第一阶段采用 Go 实现模块化单体服务，SQLite 为当前存储方案。源码在 `src/`，Server 和官方插件程序入口在 `src/app/`，模块实现按职责分目录，测试统一在根 `tests/`；Go module 与包边界见 [P0 工程布局](implementation/00-project-layout.md)。首版官方插件采用独立进程，不引入多个语言宿主。Client 播放器、主题和桌面框架由 Client 仓库设计，本仓库不规定其 UI 实现。内部依赖见[组件图](diagrams/components.puml)。

## 2. 数据模型与资产身份

| 实体 | 内部语义 |
| --- | --- |
| Recording | 内部 UUID，代表某个录音版本；Live、Remix、翻唱分别建模 |
| SourceTrack | providerInstanceId + externalId 唯一，代表平台曲目或本地文件来源 |
| SourceAsset | 内部 assetId，代表 SourceTrack 下某音质的不可变文件字节版本 |
| RecordingSourceLink | 关联、依据、置信度、人工/自动状态及修订 |
| MatchExclusion | 人工禁止关联的来源对，自动匹配不得覆盖 |
| ProviderInstance / ProviderAccount | 插件实例、能力、来源配置；账号绑定 userId、instanceId、accountId |
| ScraperInstance | 独立插件实例、能力、配置、启用状态和优先级，账号不复用 ProviderAccount |
| ScrapeJob / ScrapeCandidate | 目标、触发方式、字段、查询、插件版本、依据、状态和候选文件引用 |
| MediaObject | objectId、类型、相对路径、MIME、大小、摘要、来源、版本、状态及访问归属，不含文件正文 |
| MediaBinding | 关联来源曲目、资产或来源专辑/发行版本，记录语言、时序适用范围、人工选择和优先级 |
| Playlist / PlaylistEntry | 用户歌单及稳定 entryId、顺序、SourceTrack，允许未匹配条目 |
| PlaylistBinding | 子歌单引用、排序及来源约束，本地追加也为独立 binding |
| PlaylistSnapshot | revision、不可变条目、同步状态、生成时间、规则版本与子快照版本 |
| EntryProvenance | 原歌单、binding 与原 entryId，聚合去重后仍保留全部出处 |
| PlaybackSession / PlaybackAttempt | 会话归属、候选与恢复预算；每次尝试固定来源资产、输出表示和代次 |
| TimelineEvidence | 两端资产版本、有效区间、映射、验证方法和版本，独立于录音关联证据 |

作品层可后续添加；歌名与歌手不是唯一键，ISRC、专辑、时长和版本标识均为证据。关联不销毁来源，人工合并/解除保留审计及可逆关系，未匹配来源仍可播放。从第一版预留 userId，初期可仅启用一个管理员；全局元数据与用户的来源权限、私人文件、账号、歌单及播放状态分离，共享元数据不得泄露其他用户文件的存在性。

### 2.1 码率与 SHA-512

码率与摘要归属 SourceAsset，不将不同音质的值覆盖到 Recording 或 SourceTrack。资产身份不可变，探测结果可逐步补齐；内容变化创建新资产，已有播放尝试不静默换到新版本。

| 字段 | 语义 |
| --- | --- |
| codec、container、durationMs | 编码、封装与时长，未知为 null |
| bitrateBps、bitrateMode | bit/s；CBR 固定码率，VBR 平均码率；模式 cbr/vbr/unknown，未知码率为 null |
| bitrateOrigin | provider/probe/estimate，区分申报、探测与估算并保留来源 |
| fileSizeBytes | 完整文件大小，未知为 null |
| fileSha512 | Server 对完整原始文件字节计算，包含容器、标签和内嵌封面；存储 64 字节，协议为 128 个小写十六进制字符 |
| providerSha512 | Provider 申报的完整文件 SHA-512，独立保存；ETag、其他哈希或音频载荷摘要不能填入 |
| hashStatus、hashComputedAt | pending/verified/unavailable/failed/invalidated；verified 仅表示 Server 已完整读取并计算同一稳定版本，附时间 |

码率用于展示、筛选和辅助判断，不能单凭相同码率关联；不同编码之间不能直接比较码率推定音质。整文件包含标签/容器开销，不能简单以大小除时长代替精确音频码率。

文件级精确匹配采用已校验 fileSha512 与 fileSizeBytes，相同内容仍保留各自来源、权限和出处，不删除文件。匹配限当前用户可访问范围，人工禁止关联及冲突优先。Provider 不得标记 Server 的 hashStatus 为 verified；申报与实测冲突时保留冲突，撤销依赖错误申报值的自动证据。

转码、重新封装、不同码率或只改标签都可能改变整文件 SHA-512，哈希不同不意味着不同录音。摘要范围依据 [SHA-512 标准](https://csrc.nist.gov/pubs/fips/180-4/upd1/final)，探测器与支持矩阵仍需验证。

本地/NAS 摘要由可取消、低优先级、限并发任务流式计算，不将全文件装入内存，不阻塞首次入库或播放。任务绑定稳定读取版本，文件身份、大小、修改时间仅作失效线索；读取中变化则丢弃重试，不能确认同一版本时不得标 verified。后续变化使旧资产不再代表当前文件，撤销旧资产作用于当前文件的自动证据，历史摘要仍保留审计。

在线来源允许码率或摘要未知。仅在授权且完整读取同一版本全部字节时计算，不强制预下载每首歌，不把部分 Range、重复或乱序片段冒充完整文件。不可完整读取时记 unavailable，不阻止播放。未取得 SHA-512 时可用可靠强验证器判断播放资源是否未变，不能把新版本接在旧偏移后。

### 2.2 音频指纹与时间轴证据

后台对授权可读音频提取指纹，寻找不同编码、码率或标签版本的同录音候选。输入是解码音频，按算法规范处理，位于用户 EQ、变速、淡化等 DSP 之前。保存算法/版本、提取配置、assetId、覆盖时间范围、状态与计算时间；指纹明细存独立文件，数据库只存索引、候选分数与证据引用。

指纹相似不能单独证明同一录音或精确对齐。Library 结合版本、时长和元数据生成候选，遵守 MatchExclusion，歧义留待确认；Recording 关联不自动授予跨源续播资格。资产或算法变化后重算，旧证据保留审计。局部指纹明确范围，不能伪装全曲证据；无法读取的在线源允许缺失，不强制全库下载。

可评估 [Chromaprint](https://github.com/acoustid/chromaprint#chromaprint)，算法、阈值、误匹配率和资源消耗待验证。默认实例内检索，外部指纹服务需独立显式配置，不自动上传指纹。

TimelineEvidence 单独记录跨资产映射、有效区间及验证版本，纳入前导静音、裁切和编码延迟；处理链映射见[音频处理](audio-processing.md)。跨来源续播遵守总体架构的同录音、可定位和有效映射条件；同一 SourceTrack 内仅重建输出仍可根据经过验证的曲目到新输出映射定位，不要求先关联 Recording。

## 3. 插件宿主与 SDK

### 3.1 共用基础设施

三类 Server 插件独立注册、安装、启停、配置和授权；同包多角色不能隐式共用凭据或实例状态。Server 与 Client 分别拥有宿主 SDK，可共享模型，不承诺二进制跨运行端通用。

后续统一插件管理器将区分插件级总开关与角色实例开关，同时启用且运行就绪才能接收新工作。目标为默认平滑停用：立即关闭新任务/新播放准入，已有播放和执行中任务凭原租约完成后停止；另提供显式强制停止。禁用保留库、账号引用、人工结果与媒体文件，重新启用不恢复已被单独禁用的实例。期望开关与 starting/ready/draining/disabled/failed 运行状态分别报告；动态管理尚未实现，详见[插件管理计划](implementation/plugin-management.md)。

Server及插件实例配置已统一为YAML，启动时读取显式插件/实例双层开关。配置读写服务按修订校验并原子更新同一份YAML，SQLite仅记录操作与恢复事实，不另存权威开关；保存只返回saved与待应用状态，不直接改变运行插件。覆盖、严格解析、恢复与限制见[配置说明](configuration.md)；旧JSON配置不兼容，HTTP管理与运行时启停仍待实现。

manifest 描述 id、version、protocolVersion、roles、能力、配置 schema 和权限。请求带 requestId、instanceRef、可选 accountRef、deadline、cursor 与取消令牌；账号只能属于本次用户及角色实例。分页返回 nextCursor 和可用快照版本；错误含 AUTH_REQUIRED、ACCESS_DENIED、NOT_FOUND、RATE_LIMITED、TEMPORARY_FAILURE、UNSUPPORTED、UNAVAILABLE、retryAfter 及可重试性，不包含密钥。

控制面可评估版本化 JSON-RPC，经 stdio 或本地 socket 通信；模型和协议使用可生成绑定的 Schema/IDL，不绑定实现语言，也不要求首版同时交付多个语言宿主。音频数据采用独立有界二进制帧/受控共享缓冲，不用 JSON/Base64 传 PCM。进程内性能接口如采用，限版本化 C 兼容数据和不透明句柄；语言对象、异常和分配器所有权不跨界，不采用跨编译器 C++ ABI。

先交付官方受信任插件，第三方开放前验证权限和故障隔离。权限限定域名、挂载根、任务只读资源、执行时间、资源及日志长度；进程隔离本身不是完整沙箱，原生进程内清单也不是安全边界。官方插件同样无无限文件/网络权限。插件不直接写核心表，经 Host 持久接口保存实例映射等状态；Server 插件仅获得本角色实例已授权凭据，客户端及其插件不能获得这些凭据。

### 3.2 Music Provider

Provider 负责发现、基础元数据、音质/可用性、歌单读取和流解析，按需声明 search、metadata.read、playlist.read、stream.resolve、library.scan、recommendations.playlists，不要求全实现。登录、playlist.write、metadata.write 分别声明；能力不代表授权，metadata.write 默认关闭，首版不做平台写回。

metadata.read/stream.resolve 按具体音质/版本给出可得的 codec、container、durationMs、bitrateBps、bitrateMode、fileSizeBytes、providerSha512 和版本证据，未知为 null。resolve 另给 MIME、质量、过期时间、Range 能力、是否允许直放及仅 Server 可见请求头；核心建立 SourceAsset。Provider 不决定全局匹配、去重和来源优先级。

推荐歌单保留 Provider、来源歌单 ID、平台排序/说明、匿名/个性化范围、时间、有效期和分页。Server 分来源呈现、允许部分成功，不把多平台结果擅自解释成统一推荐算法。个性化缓存按 userId、实例和账号隔离，失效凭据返回登录状态；匿名与个性化缓存不能混用。打开推荐项按需读来源歌单，用户明确导入/绑定后才入库；推荐变化不删除已导入内容、不触发平台写回。实际可播放性仍由 Resolver 判断。

平台歌词/封面经明确注册的 Scraper 适配器进入候选流程，不让其他 Scraper 依赖在线 Provider；基础标签仍由 Provider 用于建档。

### 3.3 官方 Local Files Provider

首版支持 Server 本机及已挂载可访问 NAS，实例配置授权根、递归/排除、扫描策略及可选写回，无需在线账号。原生 SMB/NFS 直连属于后续独立验证，首版不依赖。

| 能力 | 内部行为 |
| --- | --- |
| library.scan | 初始全量、手动全量/增量、定期复核；变化通知仅为增量提示 |
| metadata.read | 读取支持格式的原始标签、codec、时长及 CBR/VBR 等，解析库/矩阵待验证 |
| stream.resolve | 限定根和版本的内部文件引用、长度/MIME/Range，Gateway 鉴权交付，不泄露原路径 |
| 受限读取 | 探测/摘要读取指定版本；本地 Scraper 获任务只读引用，提取内嵌或旁置内容 |
| metadata.write | 经配置/能力验证写标签，不自动刮削、不转码或删除音乐 |

externalId 持久分配，不用绝对路径、歌名或 SHA-512 直接作身份。经 Host 保存根、相对路径、文件系统标识和观测版本；可靠识别的重命名/移动保留 SourceTrack。歧义保留待确认候选，不把同内容多份文件强行当移动；独立文件保留各自来源/路径。

每根保存扫描代次、完整性和预期挂载/卷身份。NAS 离线、不可读或扫描取消时保留旧曲目和关联，标离线/不完整，不按本轮未见批量移除。失挂后残留空目录不算成功扫描；来源身份有效、根可访问且完整复核确认缺失才更新不可用状态，重连可恢复。文件变化创建新资产并排队探测/摘要，扫描完成不代表摘要完成。

每次打开校验实际解析路径仍在授权根下，符号链接/目录联接不得越界。Local Files 指 Server 可访问文件系统；远程 Client 的本机路径不会自动被远程 Server 读取，上传/共享另设计。Docker 配置容器内路径，音乐默认只读。

### 3.4 独立 Scraper

Scraper 补充信息，不创建音乐来源或提供音频流。按需声明 lyrics、artwork、album.metadata，通过 scrape.search 查询候选、scrape.fetch 取内容或受限资源；独立账号、限流、缓存和配置，Provider 登录不隐式授权其凭据。

请求带目标、歌名/歌手/专辑/时长等证据、字段、语言及用户修正，必要时授任务只读资源。返回 candidateId、插件/来源、依据、语言、格式、版本适用范围和内容能力。置信度只是证据，插件不能合并 Recording、覆盖人工结果或写原文件。

官方本地提取、独立在线和平台适配 Scraper 共用流程；已知本地来源可只给一个候选。Server 校验下载地址、重定向、大小和格式后存 Media Store，远程 URL/凭据不作永久内容 ID。平滑停用拒绝新任务，执行中项按原租约收尾；强制停止才取消执行中项，卸载需先完成停用。保留已发布内容和来源，不影响与该 Scraper 无依赖的音乐播放，具体行为遵循插件管理规则。

### 3.5 Audio Processor

audio.demux、audio.decode、audio.dsp、audio.encode、audio.mux 可由一个或多个插件提供，独立于 Provider/Scraper。Host 负责组合、格式协商、时间映射和受控运行；完整数据面、缓存和任务治理见[音频处理](audio-processing.md)。

## 4. 刮削、Media Store 与原文件写入

### 4.1 自动和手动流程

自动规则配置时机、实例顺序、字段与选用策略，默认只补缺失且依据充分的内容；歧义待选，失败保留已有内容，不阻塞入库/播放。本地 Provider 可搭配独立在线 Scraper，不必添加在线音乐账号；具体来源能力逐个验证。

手动支持单曲、来源专辑和批量目标。用户指定插件/字段、修正查询，预览来源、封面、歌词语言/时序及依据，再逐字段或按批次选用。预览不发布，应用时校验目标修订和人工锁；成功记录人工选择，晚完成自动任务不能覆盖。批量逐项反馈，允许取消、排除插件、只重试失败项。

上传封面/歌词、编辑歌词和更换选择也走核心校验/发布，不依赖第三方插件。自动、手动、上传或选用都不自动开启原文件写入。

### 4.2 文件与数据库

mediaRoot 默认在 Server 数据目录，与音乐目录分离：

```text
data/media/
  lyrics/<objectId>.lrc
  covers/<objectId>.jpg
  metadata/<objectId>.json
  fingerprints/<objectId>.bin
  staging/
```

扩展名与实际格式一致。歌词正文/翻译/逐行逐字时序、封面/缩略图、完整刮削文档和指纹明细均存文件，不存数据库 BLOB、Base64 或 JSON 正文字段。数据库只存可查询规范化字段、路径、摘要、版本、来源、绑定、修订及任务状态等索引。非 LRC 按实际格式保留信息。

路径由 Server objectId 生成，数据库存相对 mediaRoot 路径，客户端只获鉴权 URL。歌词绑定当前来源/录音版本，不能凭歌名复用时序；封面绑定来源专辑/发行版本，不因 Recording 合并覆盖版本差异。保存来源/获取时间，人工内容优先且不按普通缓存淘汰。

更新生成不可变新文件：临时写入、校验、同文件系统安全发布，再事务切换数据库引用。文件和数据库不是同一原子事务；持久记录阶段供崩溃恢复，失败留旧引用。临时/孤儿文件过宽限期回收，保护被引用或活动任务持有对象。备份含数据库、引用文件与配置，以版本清单形成一致可恢复视图；缺文件标待修复，人工内容不能当一定可重新刮削的缓存。

### 4.3 可选写回

默认 writeBack.enabled=false，启用后按本地/NAS 实例、目录与字段选择歌词/封面/标签，默认只补缺失；覆盖需显式配置，保留未选和未知标签，失败/缺内容不清空。独立文件继续保留，本配置不授权在线歌曲或平台歌单写回。

仅可写、格式支持且存储具有验证过的安全替换/恢复能力时执行；不转码，标签库与格式映射待验证。Scraper 无原文件写权限，由核心调度授权 Provider 写入。

任务绑定预期 SourceAsset，与扫描/摘要协调并检测并发修改。留可恢复备份，在原文件同一文件系统准备副本、校验后安全替换；正在播放时延后，NAS 无安全替换能力禁用，跨文件系统移动不视为原子操作。

记录阶段与前后版本，异常恢复或标待恢复，不盲重试覆盖用户新修改。成功更新身份/路径映射、创建新 SourceAsset、重探测并重算 SHA-512，不能沿用旧摘要。核心确认只改标签时保留 SourceTrack、Recording、歌单及人工关联，避免误扫成新歌；旧播放尝试不静默绑定新字节版本。

## 5. 歌单快照与任务调度

聚合为引用加可重建快照，默认按 binding 与子歌单原顺序合并；确认同 Recording 去重，未知按 SourceTrack 去重，保留全部出处和来源。本地追加为独立 binding，拒绝循环引用，编辑默认只改本地规则；平台写回冲突/补偿以后另设计。

远程刷新先完整分页到临时快照，再事务发布 revision。失败保留旧成功结果并标 stale、lastSuccessAt、errorCode，不完整抓取不能按缺失删除。有一致版本则固定/验证；无一致版本时完整分页仍不证明删除，缺失先疑似移除，经后续完整同步或独立查询复核才移除贡献。

删除子歌单只移除其贡献，其他出处存在则保留条目。隐藏规则独立持久存储，不绑定展示行、不改远程：默认隐藏 Recording 全来源，可仅隐藏 binding/原 entryId。未匹配先绑定 SourceTrack，确认关联后迁移整首隐藏意图。解除/拆分错误关联依据原目标保留意图，不扩散到所有结果；不能唯一还原时留原规则并提示确认。

按账号/实例限流，同歌单重复请求合并且串行同步，持久化代次；发布事务检查有效代次，禁止旧或恢复任务覆盖新结果。聚合保存规则版本/子快照版本，重建不得丢人工隐藏规则。

Scheduler 持久化阶段与状态，重启恢复先校验目标版本/所有权；指数退避加抖动，支持取消，凭据失效暂停对应任务并提示登录。扫描、摘要、指纹、刮削、写回分别限额，播放优先，停机可达，失败不破坏已发布内容。单写入队列不能替代任务代次和目标修订检查。

## 6. 部署、密钥与恢复

Docker 持久化 DB/media/cache/config，media 与音乐挂载分离；音乐默认只读，仅显式回写且能力验证通过的实例目录可写。插件限授权挂载根。本地可执行程序用用户数据目录，便携模式可指定独立目录，默认 loopback；远程需显式鉴权和 TLS。

Server 记录实际启动归属。client-managed 的受鉴权租约正常退出释放、崩溃超时失效；启动后始终没有初始租约也在启动超时后回收。最后租约失效进入 draining，拒绝新租约并让 Client 重新发现，取消/保存后台任务、完成文件写入安全收尾后有界退出，不因扫描永久常驻。独立启动不因 Client 连接退出改变归属。跨端握手、就绪与升级见总体架构。

升级协调停机、备份/迁移再启动，不能覆盖运行程序，失败保留恢复入口；独立部署由其部署方式管理，不接受 Client 无授权接管。

私有文件、歌单、会话按 userId 授权。代理只接收 Provider 授权来源，不作任意 URL 代理；校验 DNS、重定向和目标地址，NAS 内网按实例授权，其他入口拒绝 SSRF 目标。短期流凭据限会话/资源/有效期，来源 URL 不作永久标识。

密钥使用 OS key store 或外部主密钥，Docker secret 注入，DB 不存明文凭据；日志脱敏 token、请求头、URL 参数及私人路径。宿主只向 Server 插件提供本实例被授权凭据，不传给客户端及其插件。

SQLite WAL 配合单写入队列，记录 schemaVersion，迁移前一致性备份，不能只拷贝运行中主库而忽略 WAL。数据库与 Media Store 按发布清单共同备份/恢复；缓存可重建，人工内容须完整恢复。

## 7. Server 交付与验收

交付顺序以 [P0—P8 实施计划](implementation.md)为准：P0/P1 建立 Go 工程和服务基础，P2 实现 Local Files 与真实列表，P3 完成授权原流的 Web 播放，P4 完成音频插件链与处理后播放/定位。语言已经确定，依赖版本、输出矩阵和实际隔离能力在相应阶段验证。

P5 增补 SHA-512、指纹与人工匹配；P6 实现独立 Scraper、手动/自动选用、文件内容存储和可选安全写回；P7 按能力接入在线平台、推荐、歌单同步/聚合及严格的跨源恢复。P8 验证本地/容器交付与恢复，并提供受 Client 管理的服务端租约能力。桌面 Client 的启动器、第三方插件隔离、多用户产品流程与平台写回继续按各自需求推进，不列为本轮最小 Web 的前置条件。

验收包括：不同录音不误并、人工排除优先；同内容多路径保留出处；标签/转码变化不因哈希不同强制拆分；VBR/未知值正确；读取中变化不发 verified；错误申报不成为有效证据；无完整在线哈希/指纹仍可播放；匹配和推荐缓存不跨用户泄露。

同步验收包括保守复核删除、多出处贡献、隐藏规则重建/身份迁移、旧任务不覆盖新结果及失败保留旧快照。扫描覆盖 NAS 失挂、路径越界、移动歧义、重复文件及远程 Client 路径边界。

刮削验收包括插件/账号独立、无在线 Provider 仍可刮削本地库、预览不发布、自动任务不覆盖人工、批量逐项失败、本地编辑不依赖插件。文件正文不入数据库；默认不改原音频摘要；发布中断留旧文件、回收保护引用、备份含人工文件；仅授权且有能力才回写，失败可恢复，成功换 asset/hash 但保留 track/歌单关系。

播放、缓存、seek 与处理任务验收见[音频处理](audio-processing.md)。上述均为设计，未验证选型不视为已实现。
