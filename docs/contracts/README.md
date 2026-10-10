# 共同契约

`openapi.yaml` 使用 YAML 1.2 兼容的 JSON 语法。HTTP 模型只定义在 `http.schema.json`，不从手写 DTO 自动反向生成规范。控制请求拒绝未知字段；响应允许添加可忽略字段。OpenAPI 3.1 与插件 Schema 均使用 JSON Schema 2020-12。

本目录以 P0–P4 共同边界为基础，记录后续兼容增量。文件存在不代表每个长期能力已经实现；当前本地播放、处理、定位及 NCM 解密的实际验收范围见[实施记录](../validation/progress.md)，未提供的能力仍返回 `UNSUPPORTED`。

## HTTP 语义

- ID 均为不透明字符串；客户端不得从 ID 解析路径，也不得自行拼接 streamUrl。输出不返回磁盘路径、引导凭据或插件能力 token。
- UTC 时间必须以 `Z` 结尾；曲目位置/时长为整数毫秒、大小为字节、码率为 bit/s。HTTP 错误 `retryAfter` 是秒；插件 `retryAfterMs` 是毫秒。
- `null` 表示未知、不适用或尚未探测；时长、大小的 0 是已知零值，码率只能为正整数或 `null`，不能用 0 代替未知。`artists: []` 表示尚无可展示歌手。`nextCursor: null` 为列表结束。
- 登录后修改状态校验允许的 Origin、HttpOnly Cookie 与 `X-CSRF-Token`。浏览器原生 audio 通过同源 Cookie 请求音频，无需自定义 Authorization。
- `Idempotency-Key` 按用户、路由和请求摘要隔离。同键相同请求重放；不同请求返回 `CONFLICT`。结果保留期限覆盖服务声明的重试窗口。未知结果先同键有界重试，不能假设客户端断连撤销了操作。
- 创建会话固定资产、表示和 attempt。seek/recover 对同一会话串行、先重放幂等结果再检查旧 attempt；晚到请求不能覆盖当前尝试。每次表示或起点改变都换 attempt 和 URL。
- `byteRange` 仅用于有固定版本保证的可 Seek 表示；`timeRestart` 返回新片段与 `positionOriginMs`。未验证能力返回 `none`。canPlayType 只作为请求线索，不能代替真实播放验收。
- 游标绑定用户、实例过滤、排序和已发布修订；游标失效时返回 `CURSOR_EXPIRED`，客户端重建列表，禁止拼接两个修订。
- 稳定文件流支持单范围和最多 16 段的多范围。非法语法、反向范围或超出段数上限返回 400 `INVALID_ARGUMENT`；合法范围交由 HTTP 标准库计算，无法满足时返回 416。If-Range 失配可回退为完整固定表示。处理中的流不继承输入 Content-Length/ETag/Range。

## 兼容与验证

### 插件导入选项

Provider 响应增加 `configuration` 与 `roots`。Local Files 的配置包含插件提供的 `schemaRevision/importOptionsSchema/importDefaults/unavailableOptions`；实例默认已合成到公开 `importDefaults`。根列表只含授权编号、显示名与可选安全写回能力，不含路径、真实文件系统身份、能力票据或内部配置摘要。当前 Web 列表/播放允许忽略新响应字段，本步骤没有导入表单 UI。

创建扫描必须提交 `{mode,schemaRevision,rootIds,options}`。`rootIds` 为 1—256 个不重复的已授权根编号，`options` 可为 `{}` 或部分覆盖；不兼容旧的仅 `{mode}` 请求。实例与请求层都遵守[递归归一规则](../configuration.md#导入选项)：默认 true/64/false，关闭规范为 0，从关闭重新开启且未给深度恢复 64，开启深度 1—64。类型错误、矛盾深度或未知字段为 400；不属于当前实例的根为 403；旧 schema 修订为 409。当前 `local-import-v2` 支持 `importLyrics=true`，扫描成功后独立处理歌词，结果另查 metadata-results。

任务只合并同所有者、实例与有效计划的在途请求；同幂等键改变选中根、选项、模式或绑定版本返回 `CONFLICT`。根顺序不改变计划。读取任务新增 `rootIds/effectiveOptions/schemaRevision`，内部计划摘要和根身份不公开。`complete` 表示所选范围完整完成；未选根和范围之外的更深曲目保留。排队后有效计划持久固定，重启配置改变不能静默改用新参数。

### S-P5-01 当前资产证据

`SourceTrack.asset` 是必需的当前原始资产对象；即使媒体信息未知也存在非空 `assetId`。顶层 `assetId`、`codec/container/durationMs/bitrateBps` 保留为当前资产投影，稳定 `sourceTrackId` 不随编码、音质或标签修改而改变。原文件版本变化会创建新资产并使旧证据失效；旧证据不出现在当前资产的 `evidence` 中。历史查询仅为宿主内部、按所有者限制的审计接口，本步没有公开历史 HTTP 端点。

`bitrateOrigin` 区分 `provider` 申报、`probe` 实测、`estimate` 估算和 `manual` 人工信息；`bitrateMode` 为 `cbr/vbr/unknown`，质量档位名称不能填入模式或当作实测码率。当前本机探测不能证明 CBR/VBR 时返回 `unknown`。`providerSha512` 仅为 Provider 对原文件的申报；`fileSha512` 只有宿主完整文件验证证据为 `verified` 时才有值，两者不能互相替代，也不是解密后音频或 PCM 摘要。摘要固定为 128 个小写十六进制字符。

相同资产重扫时，Provider 撤销的申报不再作为当前证据；成功探测缺值或切换来源后撤销对应旧探测/估算值。失败或未尝试的探测保留同一资产最近成功的证据，人工值和完整文件摘要不受该协调过程覆盖。ffprobe 证据记录本次工具输出的版本；原始版本不符合证据标识符字符/长度限制时，以 `sha256-<原始版本摘要>` 稳定表示，避免发行版本后缀使扫描失败。

`hashStatus=unknown` 表示尚无摘要任务；`pending` 表示已登记持久任务；`unavailable/failed` 区分不可获得与执行失败。除 `verified` 外，`fileSha512/hashComputedAt` 均为 `null`，具体失败由证据的 `failureCode` 表达，不能用零码率或空摘要掩盖。证据携带资产 ID/版本、代次、算法/算法版本、配置修订、范围与 `evidenceVersion=1`。范围为半开区间；摘要证据必须覆盖完整原文件。跨字段绑定、范围上限、预期版本/代次和过期任务拒绝由宿主及存储事务校验，JSON Schema 不代替这些运行时保证。

本项目尚未上线，本次直接更新 v1 开发基线：`SourceTrack.asset` 及插件观测的证据字段是必需项，不承诺旧契约兼容。现有最简 Web 的列表检查允许额外响应字段，继续使用原有投影字段播放；此步不增加证据界面。开发基线确定后再按下面的发布兼容规则演进。

### S-P5-02 完整文件摘要任务

`POST /source-tracks/{id}/hash-jobs` 接受 JSON `{}` 和 `Idempotency-Key`，返回 202 `{jobId,merged}`。手动任务不受 `hashing.auto=false` 关闭自动排队的影响；并发与所有任务共享的速度预算见[配置说明](../configuration.md)。同用户、路由与键重放已有任务，目标资产版本变化后同键请求返回 `CONFLICT`，过期键返回 `IDEMPOTENCY_EXPIRED`；其他键可以合并同资产在途任务。任务结果重试窗口为一小时，返回 ID 可继续查询已持久化任务。

`GET /hash-jobs/{id}` 返回固定 `sourceTrackId/assetId/assetVersion`、`status`、`progress.completedBytes/totalBytes`、`error` 与创建/更新时间。状态为 `queued/running/succeeded/failed/unavailable/cancelled/superseded`；字节已全部读取仍须通过稳定完整读取校验才能成功。`POST /hash-jobs/{id}/cancel` 同样要求 JSON `{}`、Cookie、允许的 Origin 和 CSRF，自然幂等，无需幂等键。重复取消终态任务返回原终态，不更新完成时间或重新排队；并发完成先发生时可返回 `succeeded`。本人以外的任务和来源统一 `NOT_FOUND`，不暴露路径、内部票据或读取权限。任务错误中的 `requestId` 对应当前 HTTP 请求。

成功摘要仍从当前 `SourceAsset.fileSha512` 读取，覆盖原始文件的容器、标签及内嵌封面，不能替换为解密输出、PCM 或局部片段。完整文件摘要与 Provider 申报冲突时，`asset.hashConflict` 返回 `conflictId/providerEvidenceId/verifiedEvidenceId/reason/detectedAt/resolvedAt`，原因固定 `PROVIDER_SHA512_MISMATCH`，当前未解决冲突的 `resolvedAt=null`；无当前冲突则为 `null`。错误申报仍作为申报证据保留，不能进入已验证摘要匹配，历史冲突仅留在内部审计。哈希相同不删除文件、不合并来源权限，也不能单独证明跨源续播时间轴。

新增任务与冲突字段按本开发基线同步更新；Web 可忽略这些响应字段，当前没有哈希管理页面。真实文件稳定读取的系统限制和验收范围以[识别证据说明](../identification.md)及[验收记录](../validation/identification.md)为准。

### S-P5-03 本地片段指纹

启用条件与工具固定摘要见[配置说明](../configuration.md)。`POST /source-tracks/{id}/fingerprint-jobs` 接受 `{ "startMs": 0, "durationMs": 120000 }` 和 `Idempotency-Key`，返回 202 `{jobId,merged}`。起点为 0—86400000 毫秒，请求时长为 10000—120000 毫秒；二者必填。算法固定为 Chromaprint 1.6.1 / algorithm 2，先按 DSP 前音频规范化为 11025 Hz、mono、s16le。请求不能指定工具身份、算法、路径、上传或外部检索。功能默认关闭；未配置或启动时工具校验失败，对有效请求明确返回 415 `UNSUPPORTED`，普通播放继续可用。

`GET /fingerprint-jobs/{id}` 返回固定的来源、资产版本、请求范围、提取器身份和任务状态；成功的 `result` 是索引，包含实际覆盖样本、质量和媒体对象摘要，不展开原始指纹值。状态为 `queued/running/succeeded/unavailable/failed/cancelled/superseded`。`POST /fingerprint-jobs/{id}/cancel` 接受 `{}`，遵循 Cookie、Origin 和 CSRF，取消自然幂等且不要求幂等键；完成先发生时返回已完成结果。重复取消终态不更新完成时间。

`GET /source-tracks/{id}/fingerprints?limit=50&after=…` 只返回当前资产有效索引，`limit` 为 1—100，`nextAfter=null` 表示本轮分页结束；源文件离线仍可查询已持久索引。分页采用稳定 ID 顺序，不保证并发变更期间的快照。`GET /fingerprints/{id}` 经所有者授权后显式读取有界 JSON 明细，包括至多 1000 项 uint32 指纹值；文件存于 MediaStore，SQLite 只保存索引、摘要、引用和发布状态。保留的历史明细可按 ID 审计，历史索引标记 `current=false`；路径、读取票据、凭据和内部任务代次不公开，其他所有者与不存在都返回 `NOT_FOUND`。

质量 `short` 表示实际覆盖不足十秒，`lowInformation` 表示信息量不足，`available` 仅表示通过信息量门槛；它们都不是歌曲匹配结论。实际 EOF 不补零，不把请求时长冒充读到的样本数。原文件 SHA-512、NCM 外层资产与解密后局部音频指纹保持不同含义；本步没有外部上传、平台识别或 Web 指纹管理界面。HTTP、YAML、真实工具及播放回归范围见[指纹接口验收](../validation/fingerprinting-http.md)。

### S-P5-04 候选与人工关联

`POST /source-tracks/{id}/match-candidates/refresh` 接受可选 `sourceTrackIds` 比较池；省略或空数组时选最多256条本人来源，显式池不包含锚点或重复项。刷新自然幂等，不要求幂等键，不推进人工修订；存储在发布前复核资产、证据与人工修订。`GET /source-tracks/{id}/match-candidates` 只返回当前 `pending` 结果，默认20条、最大100条。所有候选 `requiresConfirmation=true`；排序分数与指纹 BER 均不是概率，不产生 TimelineEvidence。

`GET /matching/revision` 提供本人全局修订。四项人工 POST `/matching/links`、`/matching/unlinks`、`/matching/splits`、`/matching/exclusions` 要求必需的 `expectedRevision`、幂等键和既有会话/Origin/CSRF；返回新修订、操作 ID、可空录音 ID 和变更审计。幂等窗口24小时，同键同规范化请求重放原操作，过期或冲突明确报错。关联只移动选中来源，拆分要求非空真子集；排除不能暗中解除已有同录音关系，关联不能越过有效排除。

`GET /recordings/{id}` 与 `GET /source-tracks/{id}/recording-link` 查询本人关系；其他所有者与不存在都返回 `NOT_FOUND`。`GET /matching/changes` 按本人 `sequence` 提供默认50、最大200条审计，保留原目标/新目标或排除两端，供后续可逆迁移使用。本步不删除来源、不扩张权限、不迁移歌单隐藏意图，不增加 Web 匹配管理页面。完整操作说明见[人工关联](../matching.md)，测试边界见[接口验收](../validation/matching-http.md)。

### S-P6-01 歌词与封面内容

`POST /media` 接受原始正文和 query 中的 `sourceTrackId/assetId/assetVersion/field/language`，要求 Cookie、Origin、CSRF 和幂等键。上传成功只得到预览对象，不能直接改变人工选择。歌词为规范 UTF-8 文本/LRC、最多1 MiB，PNG/JPEG 最多8 MiB并完整解码；真实格式与声明 Content-Type 必须一致。歌词语言省略/空值统一 `und`，其余统一小写，封面语言为空；请求不能伪造 origin、路径、URL或实例。

`GET /media/{id}` 授权交付正文，`GET /media/{id}/info` 返回描述；均限制所有者和来源归属。上传与正文交付各有30秒期限，上传专用8 MiB预算不放宽普通64 KiB控制请求。`GET/PUT /source-tracks/{id}/media-selection` 读取/人工选用；PUT 必填 `objectId/assetId/assetVersion/expectedRevision` 并加人工锁。`POST /source-tracks/{id}/media-selection/unlock` 接受 `field/language/expectedRevision`，只解锁、不删除旧选择。两项修改要求幂等键，修订按所有者/来源隔离，幂等窗口24小时。

选择提交前验证正文，损坏新预览不能覆盖旧内容；缺失/损坏对象标记 unavailable，保留选择和人工锁。旧资产绑定 `current=false`，仍可读的旧对象为 stale；未变化的当前资产对象缺失时可为 `current=true,state=unavailable`。媒体服务不依赖指纹功能开关。本步没有 Web 编辑界面或自动抓取/写回，操作与限制见[内容说明](../media.md)、[HTTP 验收](../validation/media-http.md)。

### S-P6-03 本地补全任务

`GET /scraper-instances` 只返回本人实例的安全声明。`POST /metadata-jobs` 必需 `fields`，目标由非空 `sourceTrackIds` 或 `sourceAlbum:{sourceAlbumId,expectedRevision}` 二选一指定；两种字段同时出现（包括空数组）或显式 null 均拒绝。单曲也用1项数组；专辑入队在同一事务校验成员修订并固定成员、当前资产和选择修订，最多64来源，不截断。可选 `instanceIds/excludeInstanceIds/languages/query`；最多4选中实例、16排除实例和16语言。请求要求幂等键，返回202 `{jobId,merged}`，原专辑编号和修订进入持久请求，重放不重新展开变化后的成员。本地插件不支持非空查询修正，明确返回415；语言只按已声明标签过滤，未知为und，不推断正文语言。实例配置与操作见[补全说明](../metadata.md)。

`GET /metadata-jobs/{id}` 返回 `queued/running/completed/failed/cancelled` 状态及逐目标结果，批量部分失败必须查看各目标。候选仅含安全描述、来源依据、30分钟有效期和可空 `previewObjectId`，没有正文或内部资源票据；通过已有媒体端点授权预览。搜索不会改变当前选择。`POST /metadata-candidates/{id}/apply` 要求 `expectedRevision` 和幂等键，在当前资产、候选有效期、代次、任务取消状态及预览文件复核后人工选用并加锁。候选修订是搜索快照，后续逐字段选用可使用最新修订。

`POST /metadata-jobs/{id}/cancel` 接受 `{}`，自然幂等，不要求键；包括已完成任务的候选应用资格，可取消但不删除已发布内容/锁。原成功应用键仍重放，新应用拒绝。配置停用后历史任务和内容仍可读取；尚未提供Web补全页。真实插件、NCM、重启和权限范围见[接口验收](../validation/metadata-http.md)，来源专辑和派生文件增量见[专辑接口验收](../validation/albums-http.md)。

### P6 来源专辑、发行和派生文件增量

`/source-albums` 与 `/releases` 提供创建、读取、完整更新和列表；名称不是身份，成员编号有序且不重复，来源专辑最多1000曲且限定同本人Provider实例，发行最多256个本人来源专辑。空集合合法，但空专辑仍须授权真实配置实例。实体修订从1开始；PUT必需 expectedRevision，成员/名称修改推进修订，来源专辑更新也推进引用它的发行修订。列表返回 `{items,nextAfter}`，默认50、limit1—100；after为上页返回的编号，来源专辑支持 providerInstanceId 过滤，未知/重复参数拒绝。列表不保证跨页固定快照。

两类实体分别有 `/cover-selection` GET/PUT、`/cover-selection/unlock` POST 和 `/changes` GET。封面修订从0开始，PUT同时要求封面 expectedRevision 与实体 expectedTargetRevision，选择成员当前资产的既有图片对象并设置人工锁。封面绑定保留原source/asset依据，成员编辑不会清掉历史选择，来源曲目的媒体选择修订也不受影响。解锁只解除人工锁，不删除图片。修改需幂等键和CSRF；changes按本实体序号分页，after为十进制序号，limit1—100。精确输入和响应见[专辑说明](../albums.md)。

`POST /media/{id}/thumbnail {}` 使用登录/Origin/CSRF，天然幂等，不要求键；返回200 `DerivedObject`，固定 `thumbnail256/v1`、PNG且长边≤256，不放大小图。`GET /metadata-jobs/{id}/documents` 返回最多1024项 `ScrapeDocuments`，仅包含已发布文件；单份完整结构化记录为 `scrape-document/v1`，最大16 MiB。`GET /media/{id}/info` 返回 `ContentObject` 或 `DerivedObject`，正文通过同一授权GET读取，文档以JSON附件交付。派生对象关系和索引不含正文、路径、能力票据或公开URL；完整文档使用文档内资源序号。文件语义、损坏处理、恢复和备份边界见[派生媒体说明](../derived-media.md)。

### S-P6-04 扫描后的歌词导入与自动补缺

`GET /scan-jobs/{id}/metadata-results` 为鉴权只读接口，无修改副作用。可选 cursor（最多256字符）与 limit（1—200，默认50），未知/重复/非法查询参数为400；不存在或其他用户的扫描为404，游标不属于当前扫描为410。返回 `MetadataAutomationPage`，每来源/资产/触发原因独立记录状态、子任务和逐字段 action，`nextCursor=null` 表示本页已到末尾。内部策略、路径、代次和并发修订不出公开DTO。

`importLyrics` 与全局自动策略独立；扫描 succeeded 不代表补齐 completed，completed 也不代表所有 action 已选用。选用遵守人工锁、只补缺、实例优先顺序和本地明确来源依据，配置及跳过原因见[补全说明](../metadata.md#导入歌词与自动补缺)，接口与真实服务证据见[验收记录](../validation/metadata-automatic.md)。

### S-P6-05 显式原件写回

`POST /writeback-jobs` 必需 `sourceTrackId/assetId/assetVersion/expectedRevision/fields/overwrite` 与幂等键，返回202 `{jobId,merged}`。字段为1—2项 `{name,language}`，name限lyrics/artwork且不能重复；歌词空语言规范为und，封面必须空。只使用当前选中内容，不接受路径或正文。配置默认关闭，覆盖须配置和请求双重选择；越权、旧资产、旧修订、不支持的媒体/存储分别明确拒绝。

`GET /writeback-jobs/{id}` 返回所有者范围任务。`POST /{id}/cancel` 与 `POST /{id}/recover` 接受 `{}`，自然幂等无额外键，仍校验 Origin/CSRF。cancel只允许queued或原cancelled；recover检查当前授权后安排同任务后台重试，200返回当前状态，应继续GET轮询；succeeded仅在日记也完整完成后才对外显示。recovery保留来源屏障，不能当作普通失败绕过。

Provider 根可选 `writeBack` 为 `{enabled,available,formats,fields,replacement}`：enabled表示开关与授权，available表示实际本地存储和探测条件，仍不能替代逐文件检查。首版只支持Windows固定本地NTFS的受支持FLAC，replacement为recoverableReplace，不承诺NAS或单次原子替换。客户端可忽略该可选字段，现有Web未增加写回入口。范围、恢复和备份见[写回说明](../writeback.md)及[接口验收](../validation/writeback-http.md)。

### S-P7-02 匿名在线资料

`GET /online-instances` 只返回本人在线实例的安全声明，账号固定空匿名；available与能力supported分开。查询端点为 `/online-instances/{id}/search?q=&limit=&cursor=`、`/tracks/{externalId}`、`/recommendations?limit=` 和 `/playlists/{externalId}`。默认limit20、范围1—50，未知/重复query拒绝；搜索q最多256 UTF-8字节，cursor最多1024。上游游标绑定owner/实例/查询/limit且有时限，不声明稳定快照。

公共返回新增 `OnlineInstances/OnlineSearchPage/OnlineTrack/OnlineRecommendationPage/OnlineObservation`。资料页URL与音频流分离；playbackAvailability恒unknown，metadataDigest不成为asset版本或身份匹配证据。歌单保留重复出现的位置、最多10000条，complete只是观测数量完整且consistency恒unversioned。推荐为匿名第一页，personalized=false、nextCursor=null。所有这些读取不会隐式导入个人曲库。

`POST /online-instances/{id}/tracks/{externalId}/resolve {}` 仍鉴权及CSRF；不产生资源、无幂等键，目前明确415。Apple目录返回401 AUTH_REQUIRED，QQ未实测成功的能力不宣告supported；其他平台/限流/离线错误保留本次请求编号与可选秒单位Retry-After。不存在或其他owner实例404；路径、原始上游错误和凭据不公开。参数、能力和实测边界见[在线说明](../online.md)及[HTTP验收](../validation/online-http.md)。

### S-P7-03 推荐聚合与缓存

`GET /recommendations` 使用登录会话，返回 `Recommendations {sources:[...]}`；可重复传不同 `instanceId`（最多8项）、limit默认20且1—50、refresh严格为true/false。省略实例时选择本人配置，超过8项须显式选择；返回保持所选来源和平台项目顺序。账号当前恒空、personalized恒false、nextCursor恒null。只读刷新不要求CSRF或幂等键，不进行导入、播放或远程写回。

来源错误是每项 `error:{code,message,retryAfter}`，整体200仍须逐项检查；从未成功时items为空且时间为空，刷新失败保留上次成功项目与UTC时间并标stale。refresh不能绕过失败冷却。owner/实例/账号/配置修订/limit共同隔离缓存，重启清空内存；没有声称账号登录或个性化推荐已实现。限额、响应语义和验证范围见[推荐说明](../recommendations.md)与[接口验收](../validation/recommendations-http.md)。

### S-P7-04 远程歌单导入与同步

`POST /remote-playlists` 必填instanceId/accountId/externalId，账号显式空匿名，返回201 `{playlist,merged}`；只持久导入意图，不访问远端。`GET /remote-playlists` 返回 `{items,nextAfter}`，limit1—100默认50，GET `/{id}` 返回绑定、当前本地修订与状态；其他所有者与不存在均404，停用后历史仍能读取。

`POST /remote-playlists/{id}/sync-jobs` 必填expectedBindingRevision并带幂等键，返回202 `{jobId,merged}`；GET `/remote-playlist-sync-jobs/{id}` 读取queued/running/succeeded/failed/cancelled，嵌套error携带本次requestId。POST `/{id}/cancel {}` 自然幂等无key，仍需CSRF。入队、执行及发布均核对范围/配置/代次，失败和取消保留旧快照。

`GET /remote-playlists/{id}/snapshots/{revision}?offset=0&limit=50` 固定本地修订分页。当前consistency恒unversioned、remoteVersion恒null，重复曲目保留不同entryId；首次完整观测缺失仅suspectedRemoved，后续较晚完整观测才确认移除。RemoteSource无Asset，播放能力固定unsupported，metadataDigest不成为音频版本。首版只有已验证网易公开歌单可导入；参数、限额和幂等边界见[操作说明](../remote-playlists.md)与[HTTP验收](../validation/remote-playlists-http.md)。

### S-P7-05 个人歌单、出处与隐藏

`POST/GET /playlists` 与 `GET/PUT /playlists/{id}` 管理本人有序规则。binding.kind为local/remote/playlist；显式追加SourceRef恰一local或remote身份，新bindingId/entryId为空由宿主生成，既有ID不能换目标。PUT必填expectedRevision；ruleRevision与hideRevision独立。

`POST /playlists/{id}/rebuild {expectedRuleRevision}` 返回PlaylistSnapshotReceipt；`GET /{id}/snapshots/{revision}?offset&limit` 按固定本地修订读取可见项和全部provenance，limit1—100，旧修订不可变且每次重新授权来源。删除绑定只移除该出处，不清其他贡献；确认同Recording才去重，未知来源保持原身份。

`GET/POST /{id}/hidden-rules` 与 `PUT /{id}/hidden-rules/{ruleId}` 保存原SourceRef锚点和allSources/binding/entry范围。创建默认allSources，必填expectedRevision/source；更新必填expectedRevision/active/confirm。关联扩大后needsConfirmation，新成员不会自动隐藏；显式active=true,confirm=true才扩大，解除/拆分保留锚点意图。所有修改需幂等键和CSRF。`GET /{id}/changes` 返回独立修订审计，after是十进制序号。完整限额、重放与分页语义见[个人歌单说明](../playlists.md)和[接口验收](../validation/playlists-http.md)。

### NCM 解密增量

本次沿用既有 `/api/v1` 端点、请求和鉴权规则。播放描述的 `effectiveProcessing` 新增 `{ "type": "ncmDecrypt", "gainDb": 0, "version": "1", "execution": "server" }`。Schema 的 type 原已允许扩展字符串，无需收紧枚举或增加必填字段；客户端可以忽略未知处理项。该项表示外层容器解密，`gainDb: 0` 不代表执行了增益 DSP。后续进入转码管线时再报告独立 `gain` 项；用户未请求衰减时其值也可为 0。

SourceAsset 和 `assetId` 始终指向原始 NCM；列表保留 `container=ncm`、`mime=audio/x-ncm`，内部 codec 来自解密内容，时长/码率仅采用实际音频探测结果。播放 `outputProfile` 描述本次可交付表示：纯解密 MP3 为 `proxy/byteRange`，解密后转码为 `processed`；时长已知时可 `timeRestart`，否则为 `none`。输出表示拥有独立长度、ETag、representationId 和 attempt URL，Range 偏移相对于解密后的表示，不能复用加密文件的长度、摘要或字节偏移。

解密内容在发布前写入有配额的私有快照并复核源版本；原始文件只读。新定位仍生成新 attempt/URL，旧尝试和跨登录会话访问按既有规则拒绝。损坏文件或未启用解密插件返回 415 `UNSUPPORTED`，不能把密文字节冒充可播放音频。具体 Worker 协议见[插件契约](plugins/v1/README.md)，实测范围见 [NCM 说明](../ncm.md)。

### 兼容规则

同一 API/插件主版本只添加可选字段或可协商能力。增加 required 字段、更改单位/语义、删除字段或收紧已发布枚举需新主版本。废弃字段先在文档标记，保留一个兼容窗口并为旧消费者保存样本；不把未识别 capability 当成功。未知插件主版本在握手阶段拒绝。

执行 `go test ./tests/contract/api`。测试编译全部 HTTP/插件模型、验证有效/无效/新增响应字段兼容样本，并校验 OpenAPI 路径、引用和幂等/CSRF 声明。该测试的 OpenAPI 检查是项目结构校验，JSON Schema 模型验证由独立库执行；不将项目结构检查描述为完整 OpenAPI 官方元规范验证。后续 API handler 集成测试须调用 `tests/testkit.ValidateHTTP` 校验真实序列化响应，防止样本与 DTO 各自通过但相互漂移。

验证库为 `github.com/santhosh-tekuri/jsonschema/v6`，用于契约测试和运行时受限插件导入选项校验；当前固定版本与许可证见[依赖记录](../dependencies.md)。运行时声明最多 32 KiB、8 层与每对象 32 个属性，采用关键字白名单，不接受外部 `$ref`、脚本或任意扩展；Schema 不通过网络装载依赖。NCM 增量另由 `go test ./tests/contract/plugins` 验证有效/无效 wire 样本，真实 Worker 与 Server 测试校验实际序列化结果。
