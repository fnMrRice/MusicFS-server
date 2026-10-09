# Server 插件分阶段实现计划

状态：2026-10-09，Go 插件协议、Local Files 与音频 Worker 已有可运行实现；具体提交和实测见[插件验证](../validation/plugins.md)。本文件仍保留独立 Scraper、在线 Provider 等未实现任务；服务端业务与数据库顺序见 [Server 实现总览](../implementation.md)，跨端门槛见[总体实现计划](../../../docs/implementation.md)。

设计约束来自 [Server 架构](../architecture.md)和[音频处理设计](../audio-processing.md)。本文保留计划边界，已存在代码及已验证能力以验证记录为准。阶段编号与 Server / Web 共用；源文件尽量不超过 400 行，按职责拆分。

## 1. 首版边界与交付顺序

| 阶段 | 插件侧交付 | 对应用户可见结果 |
| --- | --- | --- |
| P0 | 明确进程协议、能力及资源引用；建立失败样例 | Server / Web 不用猜测插件行为 |
| P1 | 配置、进程管理测试基础、受限资源 Broker | 运行环境和插件错误可诊断 |
| P2 | 受信任 Go Worker 宿主与 Local Files Provider | 文件列表可持久入库、重新扫描 |
| P3 | 本地资源解析与稳定版本的原流交付 | Web 可播放原本兼容的文件 |
| P4 | FFmpeg 音频 Worker、PCM 增益节点、编码输出 | Web 可播放经服务端处理的文件 |
| P5 | 探测、完整读取、DSP 前指纹提取支撑 | 展示码率，后台计算 SHA-512 / 匹配候选 |
| P6 | 独立本地 Scraper、候选和受控写回 | 歌词 / 封面单独保存，手动选用 |
| P7 | 网易云 / QQ / Apple Music 分别验证接入 | 在线来源、可选推荐与歌单进入统一模型 |
| P8 | Worker 故障、版本升级、打包和平台矩阵 | 可复现部署及明确支持范围 |

P3 是首个可交付的播放闭环，P4 是音频处理闭环。P5～P7 不阻塞前两个闭环验收。第三方插件市场、热替换运行中的音频节点、原生 SMB/NFS 客户端、多语言 SDK、动态原生 ABI 均不列为首版前置任务；预留版本边界不等于提前建完整框架。

首版采用一个 Go module、多个 `src/app` 入口，官方 Worker 和 Server 同仓管理。控制面使用版本化 JSON-RPC 2.0，经 stdio 传输；PCM、压缩音频及文件正文走独立二进制数据通道。内置小型 DSP 通过 Go 注册表组合，不使用 Go 标准库 `plugin` 加载 `.so`。该标准库对操作系统及宿主 / 插件构建一致性有限制，不作为 Windows / Linux 共用方案。[Go plugin 文档](https://pkg.go.dev/plugin)

进程协议是扩展边界；独立进程本身不构成文件系统或网络沙箱。P0～P8 只加载管理员配置的官方受信任插件。权限检查约束 Host 提供的资源服务；第三方不可信代码开放前还需验证操作系统隔离，不能将清单权限写成已具备强隔离。

## 2. 拟建代码与测试布局

```text
src/
  app/
    musicfs-plugin-local/main.go
    musicfs-plugin-scraper-local/main.go
    musicfs-plugin-audio-ffmpeg/main.go
  pluginproto/
    manifest.go                 # Manifest and role capabilities
    envelope.go                 # RPC envelopes and errors
    provider.go                 # Provider request and response types
    scraper.go                  # Candidate and fetch descriptors
    audio.go                    # Pipeline negotiation and wire DTOs
    framing.go                  # Bounded control-message framing
  pluginhost/
    registry.go                 # Explicitly configured workers
    process.go                  # Start, handshake, exit, restart
    rpc.go                      # Request lifecycle and cancellation
    resources.go                # Scoped filesystem and stream resources
    credentials.go              # Per-instance credential access
    state.go                    # Namespaced plugin state requests
  plugins/
    localfiles/                 # Root scan, observation, identity, resolve
    scraperlocal/               # Sidecar and embedded metadata extraction
    audioffmpeg/                # FFmpeg adapters and pipeline assembly
    netease/                    # Added only when P7 begins
    qqmusic/                    # Added only when P7 begins
    applemusic/                 # Catalog / authorized account capabilities
  audio/
    pcm.go                      # Shared PCM contract
    nodes.go                    # Shared processing-node interface
    dsp/gain.go                 # Registered gain DSP
tests/
  unit/
    pluginproto/framing_test.go
    pluginhost/resources_test.go
    plugins/localfiles/identity_test.go
    plugins/scraperlocal/sidecar_test.go
    audio/dsp/gain_test.go
    plugins/audioffmpeg/decoder_test.go
  contract/plugins/             # Versioned wire compatibility
  integration/plugins/          # Real worker process lifecycle
  testkit/                      # Helpers imported by test packages
  testdata/                     # Sanitized protocol and audio fixtures
```

`main.go` 只装配配置、日志、信号和 Worker 运行循环；扫描、协议、节点处理不得堆在入口。按用户选定布局，源码统一放 `src`，入口放 `src/app`，测试全部放根 `tests`；每个源码包仍按职责使用 `scan.go`、`resolve.go`、`identity.go` 等文件。跨进程集成测试以 helper process 启动真实协议入口，协议 / 音频样例放 `tests/testdata`。这是本项目布局选择，不能称为 Go 唯一标准目录；完整布局和测试命令以[项目结构计划](00-project-layout.md)为准。

测试与被测源码在不同目录，属于不同 Go package，使用导出的业务接口进行黑盒验证；相同 package 名称不赋予访问未导出符号的权限。测试辅助代码放 `tests/testkit`，不为测试任意公开内部字段或新增源码目录中的测试文件。

先按阶段建立实际使用的包和入口，不一次创建所有空目录。`src/pluginproto` 暂不作为外部 SDK 发布；首版由本项目使用，保留带版本的线协议，不承诺源码包的外部兼容性。准备公开 Go SDK 时另建 module、兼容性策略、示例和一致性测试，不把未承诺稳定的源码路径作为对外接口。

## 3. P0：冻结最小插件契约

依赖：Server 与 Web 明确文件列表、播放描述和错误模型。统一在 `docs/contracts/plugins/v1/` 维护协议说明 / 样例，其中 `protocol.schema.json` 定义握手、manifest、控制请求 / 响应、分页、取消及错误；其他章节引用这一份 Schema，不另立路径或重复定义。P0 不以实现所有能力为完成条件。

### PL-P0-01：区分插件、角色、实例、账号

1. 定义 `PluginManifest`：`pluginId`、`pluginVersion`、`protocolVersion`、受支持的系统 / 架构、角色、能力、配置 Schema、请求和资源上限。
2. 定义 `instanceId` 和配置修订；一个插件可有多个隔离的实例。同一可执行程序的 Provider / Scraper 角色也必须显式注册并分别授权。
3. `accountRef` 指向 Host 持有的账号授权，不直接等于 token。请求附 Host 注入的用户 / 实例上下文，插件不得通过修改请求字段切换到其他用户。
4. 首版每角色实例采用独立 Worker，先避免账号与配置状态串用；是否合并进程留到实测资源开销后决定。
5. 为“不支持能力”和“支持但当前未授权”定义不同返回。UI 展示依据有效能力与账号状态，不能仅看插件名称。

拟建文件：`src/pluginproto/manifest.go`、`provider.go`、`scraper.go`；验收样例包括合法本地实例、未知角色、主版本不兼容、账号不属于实例、重复插件 ID。

### PL-P0-02：定义有界控制通道

1. 采用 `Content-Length` 标头 + UTF-8 JSON 消息体；写明长度单位为字节、消息最大尺寸、读取超时及空输入 / EOF 的语义。
2. stdio stdout 仅承载协议，英文日志写 stderr，由 Host 限长、限速、脱敏采集；启动横幅不能污染 stdout。
3. 双向调用分别使用独立前缀的 request ID。Host 请求 Worker 方法；Worker 需要资源或实例状态时请求 Host 方法，不能直接访问业务数据库。
4. 请求上下文携带 `requestId`、实例、账号引用、相对超时预算和取消关联。业务数据使用明确 DTO，避免任意对象透传进核心。
5. 在 JSON-RPC error 的结构化数据中返回业务 `code`、`retryable`、可选 `retryAfterMs`，数值 error code 与业务 code 的映射写入 Schema。
6. 明确取消竞争：收到结果后取消为幂等；取消后到达的结果可回收但不得发布；同一 ID 重复终态视为协议错误。

初始建议上限为单控制消息 1 MiB、分页 200 项、在途请求数可配置；这些值在 P2 / P4 基准后记录实际配置。文件正文、封面和 PCM 不因消息上限而被分片塞进 JSON。

拟建文件：`src/pluginproto/envelope.go`、`framing.go`，`tests/unit/pluginproto/framing_test.go`。测试拆包、粘包、UTF-8、超长声明、截断、重复 ID、未知方法、慢输入和取消后的晚结果。

### PL-P0-03：定义资源引用与二进制传输

1. 定义 `ResourceRef`：Host 生成的不透明 ID，绑定实例、任务、资源版本、允许的读取范围及过期条件；其字符串不能解释为磁盘路径或外部 URL。
2. 文件 Provider 可通过 Host 的根目录枚举服务获取受限 `entryRef`；Scraper 只获当前任务的音频 / 旁置文件引用；音频 Worker 只获本次资产输入和输出通道。
3. 定义 open / read / close、EOF、取消、版本不符及读取失败。授权与实际打开都校验，不能只在登记路径时检查一次。
4. 二进制通道单独建立，首版可使用 loopback 连接、短期 capability 握手与有界帧；具体传输验证 Windows / Linux 后冻结。控制消息只描述通道引用。
5. 任何授权地址仅绑定指定资源和任务；不得由 Web 或插件传入任意 URL 让 Host 代理。资源撤销、任务取消、Worker 退出均关闭通道。
6. PCM 帧同时声明 sample format、sample rate、channel layout、frame count、时间戳 / timebase、序号；校验长度与格式一致，显式结束帧不能与暂时无数据混淆。

测试覆盖过期引用、跨实例复用、超范围读取、错版本、越界长度、断连及慢消费者。普通进程可访问其 OS 权限内资源这一事实须在交付说明中保留，Broker token 不被宣传为系统沙箱。

### PL-P0-04：冻结首批方法及错误

| 角色 / 方法 | 输入 | 输出和副作用 |
| --- | --- | --- |
| `host.hello` / `worker.ready` | 协议版本范围、启动随机值、配置修订 | 协商版本、实例身份、实际可用能力；握手完成前拒绝业务请求 |
| `provider.scan` | 授权 rootRef、scanId、generation、扫描配置、分页游标 | 文件观测页、nextCursor、根身份与完整性；不直接发布音乐库 |
| `provider.metadata` | externalId、预期资源版本 | 基础标签、格式 / 码率等可知信息；未知值为 null |
| `provider.resolve` | externalId、音质、预期资产版本 | ResourceRef 或受控来源描述、版本证据、MIME / Range / 期限 |
| `scrape.search` | 目标证据、字段、语言、用户修正 | 候选及依据；不修改当前选择 |
| `scrape.fetch` | candidateId、字段、任务上下文 | 受限内容通道 / 获取描述、真实格式与适用范围 |
| `audio.describe` / `audio.prepare` | 输入格式、期望链及 profile、定位目标 | 可用节点、已接受图、输出与定位能力或明确错误 |
| `audio.start` / `audio.cancel` | taskId、输入 / 输出通道、起点 | 进度、终态和实际时间映射；不自行建立用户会话 |
| `host.state.get` / `host.state.compareAndSet` | 实例命名空间、key、预期修订、值 | 限额状态；不得任意查询 / 修改核心表 |

首版错误至少含 `AUTH_REQUIRED`、`ACCESS_DENIED`、`NOT_FOUND`、`UNSUPPORTED`、`SOURCE_OFFLINE`、`RESOURCE_CHANGED`、`RATE_LIMITED`、`TEMPORARY_FAILURE`、`CANCELED`、`PROTOCOL_MISMATCH`。重试依据操作类型和状态决定，不能将所有错误都当临时故障；写回等有副作用方法不能因 Worker 退出盲目重试。

## 4. P1：准备宿主运行基础

依赖：[基础服务计划](01-foundation.md)的配置、日志、任务取消和存储边界。P1 为 P2 / P4 共用能力，不实现通用插件市场。

### PL-P1-01：显式配置与依赖核验

1. 为官方 Worker 在 [YAML 配置](configuration.md)中登记可执行文件路径、校验摘要、允许版本、实例配置及插件/实例启用状态；解析后的路径为绝对路径，不扫描当前目录并自动执行未知文件。两层开关由插件管理器统一处理，不再以是否填写路径隐式表达启用。
2. 用参数数组启动程序，禁止通过 Shell 拼接命令。环境变量只传所需项，工作目录为实例私有临时目录。
3. Provider / Scraper 凭据由 Host 获取，按请求所需的角色和账号传递；不写入 manifest、命令行、测试样例或日志。
4. FFmpeg 等外部程序缺失时只将相关能力标不可用；Local Files 发现及已兼容原流播放应能独立运行。
5. 配置更新生成新修订；不对正在运行的任务静默更改账号、根目录、DSP 参数或编码设置。

拟建文件：`src/pluginhost/registry.go`、`credentials.go`。测试错误路径、版本不符、空配置、禁用实例与敏感值脱敏。

### PL-P1-02：构建可控的测试 Worker

为测试入口添加明确模式：正常响应、延迟握手、截断消息、持续 stderr、拒绝取消、指定请求后退出、乱序响应。测试必须指定自己的子进程，不能误终止用户程序。约定启动 / 停机等待上限、重启预算及 orphan 清理方式，Windows 的子进程树管理与 Unix 分别验证。

测试 helper 不作为正式插件分发。集成测试断言进程退出和句柄 / goroutine 释放，避免只检查 HTTP 请求返回错误。

## 5. P2：宿主与本地文件 Provider

依赖：P0、P1，以及[文件入库计划](02-library-and-scan.md)的 SourceTrack / SourceAsset 和扫描代次。完成后文件列表可用，但不要求所有格式标签或摘要都已完成。

### PL-P2-01：完成 Worker 生命周期

1. 按 `disabled -> starting -> ready -> draining -> stopped` 管理实例；异常终态为 `failed`，重启生成新的 Worker generation。
2. 启动时验证可执行文件、握手版本及实例随机值；握手超时回收进程并暴露可诊断错误，不无限重启。
3. 每个在途请求登记 deadline 与 context；业务取消先通知 Worker，宽限期后关闭通道并有界停止。
4. Worker 退出时统一完成未结束请求、撤销资源引用、标记任务可恢复状态；只有幂等读可自动重试。
5. 短时间反复退出触发熔断，需冷却或人工重启；其他 Provider 及已建立的独立原流请求继续工作。
6. 停用按[插件管理补充](plugin-management.md)执行：插件与实例分别有开关；默认先拒绝新工作，保留已有播放及执行中任务的有限租约，收尾后停止。管理员另可显式强制停止。已入库音乐、已发布媒体对象和历史证据保留；当前仅有启动前 Disabled 检查，不能据此宣称完整管理已实现。

拟建文件：`src/pluginhost/process.go`、`rpc.go`，`tests/integration/plugins/process_test.go`。通过正常启动、错版本、崩溃、卡住、超量日志、重复取消、Server 停机后无遗留 Worker 验收。

### PL-P2-02：授权根目录与安全枚举

1. 管理员为实例配置一个或多个 Server 可访问根目录；为每根建立稳定 rootId 和预期卷 / 挂载身份。Web 的路径字段不得直接覆盖该配置。
2. Host 提供受控分页枚举：输入 rootRef、目录 entryRef 和游标，返回相对显示名、类型、文件系统身份线索及观测版本；输出不包含授权根之外内容。
3. 根外符号链接 / Windows junction 拒绝跟随；根内循环链接中止对应分支并记录不完整原因。
4. 每次打开重新确认最终目标与根关系；采用平台上可验证的句柄 / 路径策略并测试链接置换竞争，不能仅凭字符串前缀判断包含关系。
5. NAS 失挂后留下的空目录不能当正常空库。根身份不符、权限拒绝或挂载无法确认时标 offline / incomplete，保留既有来源。
6. 文件类型初筛按支持集合，随后验证实际内容；未知、损坏或标签无法解析的候选逐项标记，单文件失败不取消全根扫描。

拟建文件：`src/pluginhost/resources.go`，`src/plugins/localfiles/config.go`、`scan.go`。fixtures 含空库、嵌套目录、中文名、长路径、大小写差异、重复内容、软链接 / junction、模拟离线根；NAS 实测单列结果，不能以本机目录测试替代。

### PL-P2-03：持久身份与观测页

1. 通过 Host 命名空间状态保存 rootId、相对路径、可靠文件系统标识和分配的 externalId；externalId 使用稳定随机标识，不使用路径或 SHA-512。
2. 每页包含 scanId、generation、页序号、观测项和 nextCursor；同页重试产生幂等观测，核心发布前再次检查扫描代次。
3. 可靠文件系统标识确认的根内重命名 / 移动保留 externalId；同内容多文件分别保留来源，歧义只生成候选。
4. 文件大小和修改时间只是版本变化线索。内容观测改变时由核心创建新 SourceAsset，不能原地覆盖活动资产。
5. scan completion 明确根身份、遍历是否完整、错误分支和观察代次；只有成功完整复核才允许核心评估缺失。
6. 中断扫描保留 checkpoint，但续跑前校验根与配置版本；条件改变则重开代次，不拼接不同版本的观测页。

拟建文件：`src/plugins/localfiles/identity.go`、`scan.go`，`tests/unit/plugins/localfiles/identity_test.go`。验收“重启 ID 不变、确定移动 ID 不变、复制文件有独立来源、不完整扫描不移除、旧扫描不覆盖新扫描”。

### PL-P2-04：基础元数据与最小能力

初次入库允许仅有文件显示名和可用性。标签读取与媒体探测排入后台；把标题 / 歌手 / 专辑与格式探测结果分开，字段保留来源。首批格式用自生成 WAV / MP3 和合法测试 FLAC 样例验证；单个解析器失败不得把零值写成真实时长或码率。

Local Files manifest 首先开放 `library.scan`、`metadata.read`、`stream.resolve`；写回能力保持关闭。不为本地 Provider 伪造在线登录、搜索推荐或远程歌单能力。

P2 出口：两个独立本地实例相互隔离、扫描可取消 / 恢复、文件列表可分页、Worker 故障可定位。数据正文未入数据库；播放并不依赖哈希 / 指纹任务完成。

## 6. P3：本地原始音频进入 Web

依赖：[播放计划](03-playback.md)与 Web 的点击播放入口。

### PL-P3-01：实现稳定资源解析

1. `provider.resolve` 接收已授权 externalId、所选音质和预期观测版本，返回 `resourceRef`、版本证据、长度 / MIME、真实 Range 能力。
2. Host 将 resourceRef 映射到当前资源版本；Stream Gateway 经用户与 attempt 鉴权后打开，Web 仅看到返回的 streamUrl。
3. 检查解析后打开前的文件变化、删除、重命名、权限变化；状态与版本不符返回明确错误，不能换到另一文件继续发送。
4. 只读原流交付直接由 Host / Gateway 读取资源，Provider 控制进程不逐块承载音频；Worker 退出不得使已持有有效资源的原流自动失效。
5. 一旦文件内容确实变化，旧 attempt 不再以新资源回应旧 Range；保持旧打开快照的能力须实测，不支持的平台明确失败重建。

拟建文件：`src/plugins/localfiles/resolve.go`，`tests/unit/plugins/localfiles/resolve_test.go`；测试预期版本不符、零字节、不存在、无权限、打开后变化及已建立流期间 Provider 退出。

### PL-P3-02：原流联调与完成条件

以公开或自生成兼容样例验证“扫描 -> 列表 -> 创建会话 -> 原流 -> 实际浏览器发声”。确认 Range 由真实可随机读取资源提供，源版本 / 长度不符返回错误；Web 不获取本地绝对路径和 Provider 凭据。P3 不把未验证格式标为支持；缺转码能力时返回 `UNSUPPORTED`，界面保留可理解错误。

验收至少含首播、暂停 / 继续、跳转进度、播放完毕、切换另一条目和取消会话后的资源释放；记录浏览器版本与样例编码，不能只以下载到文件判定可播。

## 7. P4：FFmpeg Worker 与可组合的 PCM 处理

依赖：[服务端音频处理计划](04-audio-processing.md)、P3 的资产 / attempt 身份与公共定位契约。此阶段验证处理链，暂不发布第三方原生 DSP SDK。

### PL-P4-01：确认实际 FFmpeg 能力

1. 音频 Worker 启动时读取受信任 FFmpeg / ffprobe 的版本、构建信息、可用 demuxer / decoder / encoder / muxer，映射成能力集合。
2. 不用“安装了 FFmpeg”替代具体编码器可用性；不存在的编码器从可用 profile 删除。
3. 可执行文件路径与固定选项由配置 / 代码决定，用户只选择验证过的 profile 和参数范围；不开放任意命令参数、过滤器表达式或输入协议。
4. 建立 `audio.describe` 返回支持的输入格式、输出组合、PCM 格式和 seek 限制；Host 据此与浏览器共同能力求交集。
5. 打包时记录 FFmpeg 构建配置及对应许可证材料。参考 CLI 方案以减少首版 cgo / ABI 维护，是否以后绑定 libav 单独验证。

拟建文件：`src/plugins/audioffmpeg/capabilities.go`、`profiles.go`，`tests/unit/plugins/audioffmpeg/capabilities_test.go`。测试缺程序、缺编码器、坏版本输出、进程探测超时与同名程序替换。

### PL-P4-02：完成真正经过 PCM 节点的原型

1. 第一条完整链为解复用 / 解码子进程输出 PCM，Go Worker 的注册节点处理 PCM，再交编码 / 封装子进程；音频不经过 JSON 控制消息。
2. 首版 PCM 固定一种规范格式，例如经过验证的 float32 little-endian、交错声道；输入采样率和声道布局显式协商，转换节点可观察。
3. 在 `src/audio` 定义公共 PCM 契约和小接口 `Configure / Process / Reset / Drain / Close`，在 `src/audio/dsp` 实现并注册 bypass / gain 节点；`src/plugins/audioffmpeg` 只承担 FFmpeg 解码 / 编码适配和链组装，不重复定义公共节点接口。gain 参数限范围并处理 NaN / Inf / 越界值。
4. 通过增益样例测量 PCM 峰值 / RMS，证明处理生效；将样例中一段静音、一个标记脉冲用于时间与缓冲验证。
5. FFmpeg 自带过滤器可封装成另一个受控适配器，但不能把接受 `-af` 参数等同于已经提供任意第三方 MusicFS DSP 插件接口。
6. 编码输出包含浏览器可消费的封装。先选择一个端到端验证的组合；MP3、AAC/fMP4、Opus/WebM 是候选，最终能力矩阵以实际浏览器测试为准。

拟建文件：`src/audio/pcm.go`、`src/audio/nodes.go`、`src/audio/dsp/gain.go`，以及 `src/plugins/audioffmpeg/decoder.go`、`encoder.go`、`pipeline.go`。公共契约黑盒测试放 `tests/unit/audio`，DSP 黑盒测试放 `tests/unit/audio/dsp`，插件适配测试放 `tests/unit/plugins/audioffmpeg`，真实链的进程与通道测试放 `tests/integration/plugins`；避免一个 `pipeline.go` 包揽探测、进程、DSP、缓存和协议。

### PL-P4-03：输入 Broker、背压与进程清理

1. FFmpeg 不直接获取平台账号请求头或任意磁盘路径。首版可由 Host 提供绑定 taskId / assetId 的 loopback 输入资源，短期 capability 仅能读该资产，按实际源能力服务 Range。
2. 输入 Broker 与公共 API 分开鉴权，不接收任意代理目标；拒绝重定向到未授权地址，平台私密请求只由 Host 发起。给 FFmpeg 的输入地址不进入日志或错误正文。
3. 若使用 stdin 管道作为输入，明确它通常不可随机 seek；只对实测可顺序解码的输入开放该路径，不能将管道包装成可定位文件。
4. PCM 读写采用固定上限缓冲，满队列阻塞上游；转码输出无消费者、超时或取消时向解码 / DSP / 编码全链传播。
5. 分别并行排空 stderr、读取媒体输出与传递取消，避免等待进程退出时管道塞满。退出时回收 FFmpeg 子进程树及临时文件。
6. Worker 向 Host 上报准备 / 输出中 / 完成 / 失败及进度；仅 `EOF + 正常退出 + 必要校验完成` 标完整，半成品不能发布为可复用缓存。

测试慢读取、消费者断开、编码器崩溃、解码器长时间无输出、超量日志和取消竞争。确认缓冲 / 进程数有上界，取消后的可执行程序均退出，必要 DSP 失败不会静默旁路。

### PL-P4-04：时间定位、表示与状态恢复

1. `audio.prepare` 接收曲目位置和源资产版本；验证输入可定位、DSP 可重建且请求位于有效范围，再决定是否声明 `timeRestart`。
2. 定位时先输入 seek / 预滚解码，再将目标前 PCM 丢弃或用于预热，重建 DSP / 编码器 / 封装；返回经过测量的 `positionOriginMs` 及映射。
3. 以样例标记脉冲验证目标偏移，并纳入编码延迟 / padding。不能只将 FFmpeg 命令中的 `-ss` 数字原样当准确映射。
4. 同一配置在不同起点产物的表示不同；Host 建新 attempt / URL 后客户端清理旧缓冲，不拼接处理前后字节。
5. profile、参数、插件版本、来源版本变更均参与缓存身份；Worker 不决定是否跨用户复用缓存，Host 继续检查访问权限。
6. 需要不可重建历史状态的效果先返回不可定位；seek 失败交核心决定保留旧尝试或提示重播，不向 Web 报虚假成功。

P4 出口：至少一个原先不能按选定 Web 路径直接交付的样例，经完整处理链在整段输出生成完成前便能由实际浏览器发声；不能以先完成整曲转码再播放替代流式链验收。gain 有测量结果；支持的定位路径在实际浏览器生效；不支持的能力如实报告。记录 CPU、峰值内存、首音耗时、取消耗时与 seek 误差，不在没有测量前承诺性能值。

## 8. P5：探测、SHA-512 与音频指纹辅助

依赖：[识别与匹配计划](05-identification.md)。插件提供读数和原始证据，核心决定资产版本、匹配与人工排除。

### PL-P5-01：完善资产探测

将 `provider.metadata` / 音频探测结果规范为 codec、container、durationMs、bitrateBps、bitrateMode、fileSizeBytes、bitrateOrigin；无法可靠判断 CBR / VBR 时使用 unknown。解析已知值时验证单位及范围，不将 0 当未知。在线申报值保留其来源，不覆盖 Server 已验证读数。

用 CBR、VBR、无时长、损坏头、大封面和非音频文件样例，分别验证字段及失败状态。测试将探测完成前后的 DTO 兼容传给 Web，列表与播放不因字段从 null 变成值而更换曲目 ID。

### PL-P5-02：稳定整文件读取与摘要

完整文件 SHA-512 由 Server 核心任务计算，Worker 只提供受限可读资源和版本证据；Provider 的摘要字段命名为申报值。读前 / 读后以及必要的读取期间校验资产版本，无法确认稳定同一版本时不标 verified。部分 Range 或乱序读缓存不能冒充完整读取。

测试取消、NAS 中断、边读边修改、标签写回与并发播放。读取限速并支持播放优先；同内容多路径保留独立来源。在线不允许完整读取的资源返回 unavailable，不因此阻止合法播放。

### PL-P5-03：在用户 DSP 之前提取指纹

复用已验证解码器，另建后台 extraction 任务，输入绑定 assetId / 算法版本 / 范围，输出指纹明细文件和索引描述。算法候选为 Chromaprint，参数与阈值在样例集验证后记录；不将相似度直接变成同录音事实或续播时间映射。[Chromaprint 项目](https://github.com/acoustid/chromaprint)

fixtures 包含同录音不同编码、仅改标签、前导静音、截短、现场版、混音与翻唱。测试提取发生在 gain / EQ 前、取消不发布半文件、默认不上传外部指纹服务。匹配候选、人工禁止关联与 TimelineEvidence 的持久化属于核心实施范围。

## 9. P6：独立 Scraper 与可选写回

依赖：[内容刮削计划](06-metadata.md)的 Media Store、任务、预览 / 选用修订。Scraper 不依赖任何在线 Music Provider 账号。

### PL-P6-01：官方本地 Scraper

1. 新建 `src/app/musicfs-plugin-scraper-local`，与 Local Files Provider 独立启停和声明 lyrics / artwork 能力。
2. 根据任务资源读取音频内嵌标签，以及已授权同目录内的旁置候选；旁置匹配规则显式配置，不递归搜索任意目录。
3. 候选保存来源文件 / 字段、语言、实际格式、匹配依据、适用来源或专辑版本。只知一个候选也沿用 search / fetch 协议。
4. 解析失败标具体候选不可用，不清空已发布内容；编码不确定的歌词待确认，不用错误转换静默覆盖。
5. 插件返回内容通道或受限描述，Host 按大小、格式、摘要验证后写 Media Store。数据库保存文件索引和引用，不保存歌词正文、封面 BLOB 或 Base64。

拟建文件：`src/plugins/scraperlocal/search.go`、`embedded.go`、`sidecar.go`、`fetch.go`，对应测试放 `tests/unit/plugins/scraperlocal` 和 `tests/integration/plugins`。样例包含 LRC、纯文本、不同语言、内嵌封面、多封面、坏图像、过大正文、目录外链接和无候选。

### PL-P6-02：自动 / 手动共用协议

1. 自动任务与手动查询均提交明确字段、目标版本、插件集合及用户修正；不能由插件自行扩展到整库或额外平台账号。
2. search 结果只是候选；fetch 成功只生成预览对象，不直接修改当前 MediaBinding。
3. apply 由核心校验目标 revision 和人工锁；旧的自动任务返回时不得覆盖较新的人工结果。
4. 批量任务每目标独立成功 / 失败 / 取消；平滑停用时不再启动排队项，执行中项凭已有任务租约完成，强制停止才主动取消执行中项。两种操作均保留已发布媒体与来源记录，见[插件管理](plugin-management.md)。
5. 人工上传 / 编辑不要求插件在线；插件仅参与候选来源。缓存键区分查询、语言、实例、账号与规则版本。

验收没有在线 Provider 时本地音频仍可刮削；自动补缺、手动搜索、预览、选用、取消与重试失败项都经过同一校验路径。最小 Web 不需完整刮削页面，可用 API / 集成测试验证这些 Server 能力。

### PL-P6-03：授权本地写回

1. 写回由核心 `metadata-write-jobs` 调度，Scraper 没有原文件写权限。Local Files Provider 只有实例显式允许、目标目录可写、格式适配已验证时才声明 `metadata.write`。
2. 输入带预期 assetId、要写字段、引用内容对象和覆盖策略；默认禁用且只补缺失，明确拒绝任意路径、跨实例对象和在线平台写入。
3. 在源文件同一文件系统准备副本，保留未知标签和可恢复备份，校验内容再安全替换。替换阶段持久记录，支持 Server 或 Worker 中断后的判定。
4. 活动播放延后写回；重试前检查当前版本，用户已修改时转冲突，不再次覆盖。
5. 成功通知核心建立新资产 / 摘要任务，保留 SourceTrack、Recording、歌单与人工关系。tag-only 修改也不得沿用旧 SHA-512。
6. 选定标签库及文件系统安全替换行为需要单独样例验证；NAS 不具备已验证恢复能力则保持只读。

拟建文件：`src/plugins/localfiles/writeback.go`，`tests/integration/plugins/writeback_test.go`，达到行数边界按 prepare / validate / replace / recover 拆分。测试默认原文件摘要不变、未选标签保留、失败留原件、并发修改冲突、崩溃恢复以及成功后资产变更但曲目身份稳定。

## 10. P7：在线 Provider、推荐歌单与聚合

依赖：[在线来源实施计划](07-online-library.md)及已稳定的插件协议。各平台独立验收，不以全部完成作为本地 Web 播放发布的条件。参考项目清单和用途维护在 [Server README](../../README.md#开源项目参考)，列为参考不表示已经引入依赖或验证其全部接口。

### PL-P7-01：每个平台先交付能力记录

1. 新建该平台的能力记录：匿名 / 登录、账号所需授权、地区 / storefront、搜索、基础元数据、歌单、推荐、播放解析、格式及来源版本证据。
2. 为使用的开源参考记录仓库、准确 commit / tag、参考文件、许可证和用途，核对目标版本是否归档或停止维护；运行时依赖、代码复制 / 修改和仅接口参考分别记录。
3. 先检查真实可用入口，再按 Provider DTO 实现 Go 适配；不将第三方项目启动成无鉴权全接口代理并直接暴露给 Web。
4. 每项能力用受授权账号 / 样例联调；失败仅关闭相关能力。仅有 metadata 不得宣称支持完整播放，推荐缺失不影响其他能力。
5. 平台错误转换为稳定业务状态，原始报文仅在脱敏诊断中保留必要字段；fixture 不包含 Cookie、授权 URL、个人账号或付费内容字节。

### PL-P7-02：账号隔离、分页和播放解析

1. 将账号登录 / 刷新 / 过期映射到实例状态，凭据进入 Host 密钥存储；同平台多个账号分别限流、缓存、取消和注销。
2. 元数据 / 搜索页给出平台 externalId、来源元数据、音质列表及未知字段，不在插件内自动合并 Recording。
3. `playlist.read` 保留原条目标识、顺序和出处；有 snapshot version 时固定版本，无版本时明确标记缺少一致性保证。
4. `stream.resolve` 只返回授权可用的来源、过期时间、Range 与必要 Server 私密请求描述；不能给 Web 平台 Cookie 或凭据。
5. URL 过期与资源变化分开：刷新 URL 后仍要验证输入版本，不能把新字节接到旧偏移。
6. 网络请求设置连接 / 响应 / 总超时，按账号限流；429 使用明确退避，认证失败暂停该账号任务，不能登录失败无限重试。

拟建文件分别位于 `src/plugins/netease`、`src/plugins/qqmusic`、`src/plugins/applemusic`，按实际支持创建 `account.go`、`metadata.go`、`playlist.go`、`resolve.go`。HTTP fixture 用 `httptest` 模拟，测试放 `tests/unit/plugins/<platform>` 与 `tests/integration/plugins`；真实账号测试单独显式运行，不作为离线单元测试前置。

### PL-P7-03：网易云与 QQ 音乐适配

网易云参考 NeteaseCloudMusicApiEnhanced 的搜索、歌单和推荐请求 / 响应结构；QQ 音乐参考 QQMusicApi 的接口组织、分页和账号上下文。具体可用入口以 P7 联调为准。只实现 MusicFS 需要且用户有权访问的能力，不因参考项目包含额外功能就一并开放。

分别建立登录失效、空歌单、重复条目、多页变化、仅试看 / 不可播、无损不可得、URL 过期和限流场景。音质标签和码率以实测 / 明确申报来源保存，不把名字叫“无损”当作已校验格式。

### PL-P7-04：Apple Music 的资料接口与播放边界

Apple Music API 可用于 catalog、歌单和经过用户授权的个性化能力；浏览器完整音乐播放依赖官方 MusicKit 集成。以官方文档校验 API 和授权要求，以开源 Web Player 参考交互 / 适配方式。[Apple MusicKit](https://developer.apple.com/musickit/)、[Apple Music API](https://developer.apple.com/documentation/applemusicapi)

首步完成 catalog ID、storefront、语言、分页、账号授权状态及实际推荐能力映射；保留“可展示但当前 MusicFS 播放路径不支持”的状态。不要把 catalog / preview URL 当完整歌曲裸流，也不将受保护音乐交 Server 解码 / DSP / 转码。

若需要 Apple Music 完整播放，另交付 MusicKit 浏览器播放适配并扩展公共播放描述、授权与事件模型，经 Server / Web 联合验证后再开放。它不是已有 `direct/proxy/processed` 裸音频描述的隐式特例；未完成时 `stream.resolve` 如实返回不支持。本轮最小 Web 播放以本地授权音频和已验证可交付来源验收，不让 Apple 适配阻塞 P3 / P4。

### PL-P7-05：推荐与歌单的一致性

1. 可选 `recommendations.playlists` 接收实例、账号 / 匿名范围、cursor，返回 platformPlaylistId、标题、来源排序、说明、有效期与 nextCursor。
2. 缓存区分 userId、实例、账号、storefront / 查询参数；账号失效后不能把另一账号结果作为匿名缓存返回。
3. 不支持该能力的插件返回 `UNSUPPORTED`；某平台失败可保留其他平台结果。推荐可见不承诺歌曲可播。
4. 打开推荐按需读歌单，用户选择导入 / 绑定后才持久加入歌单库；推荐变化不删已导入内容，不发起平台写回。
5. 无一致快照版本的分页结果供核心保守同步：缺失先疑似移除，后续完整同步或单项查询确认；插件不能仅用一轮列表结果直接删除核心条目。
6. 聚合保留出处，隐藏规则和 Recording 匹配由核心负责；跨源恢复的时间轴证据独立验证，插件不能用同名或相似指纹声明可无缝续播。

验收账号缓存不串用、平台部分失败、重复页 / 游标循环可终止、扫描 / 同步晚结果不覆盖新状态、推荐仅阅读不产生平台写操作。最小 Web 可维持文件列表 / 播放，推荐与聚合通过 API 验收即可。

## 11. P8：故障注入、交付与后续 SDK 门槛

依赖：[交付计划](08-release.md)。对实际发布的插件和操作系统运行，不将计划中的适配视为已支持。

### PL-P8-01：统一插件验收矩阵

| 类别 | 必须验证的行为 |
| --- | --- |
| 协议 | 主版本不兼容拒绝启动；未知字段按版本规则处理；非法 / 超大消息不使 Host 崩溃 |
| 生命周期 | 启动超时、异常退出、重复退出、取消、停用与升级；无遗留子进程 / 句柄 |
| 权限 | 跨用户 / 跨实例资源拒绝；根外链接拒绝；日志无 token / Cookie / 授权地址 |
| 本地库 | 移动身份、复制独立、NAS 失挂、重新连接、取消扫描、旧代次失效 |
| 音频 | 原流、处理、有效定位、无能力报错、慢终端背压、必要 DSP 失败可见 |
| 内容 | 候选不发布、人工锁优先、正文只在文件、默认不写原音频、写回可恢复 |
| 在线 | 账号过期、限流、分页变化、推荐隔离、源版本变化与未支持播放的明确状态 |

测试以 `tests/unit` 黑盒单元测试、`tests/contract/plugins` 协议测试、`tests/integration/plugins` 真实 Worker 测试及 Server / Web 联调组合；只在相关变化后扩展执行。失败日志保留 requestId / jobId / instanceId 与稳定错误码，不能依靠输出私密内容定位问题。

### PL-P8-02：打包与支持清单

发布包包含实际 Worker 可执行文件、manifest、示例配置、协议版本、FFmpeg 获取 / 打包说明及开源许可记录。Windows / Linux 分别验证启动、停止、路径、NAS 与二进制通道；macOS 或其他架构只有测试通过才加入支持清单。

为每个插件记录版本、构建系统 / 架构、依赖版本和支持能力；禁用缺失能力时给出配置状态。升级先停用 / 排空，再更换 Worker 并握手，失败保留旧版入口和状态迁移信息；不原地替换仍在运行的可执行文件。

### PL-P8-03：以后公开第三方 SDK 的进入条件

只有当至少两个不同角色的官方 Worker 通过协议一致性套件、兼容策略明确、权限 / 崩溃隔离完成验证后，才评估独立 SDK module。SDK 提供版本化类型、小型示例、错误 / 取消 / 资源读取帮助函数，不泄露 Server 核心包或数据库结构。

外部插件不能因有相同 manifest 就获得运行许可；第三方开放另评估安装信任、权限执行、资源限制、升级回滚和授权撤销。高性能原生 DSP ABI、多语言 SDK、共享内存等依据测量结果推进，不作为完成当前 Web 播放的条件。
