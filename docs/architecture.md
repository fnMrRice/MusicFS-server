# MusicFS 架构设计
状态：设计草案 v0.1；2026-10-09。本文是三个仓库的架构事实来源，未实现的功能不得描述为已完成。

## 1. 目标与边界
MusicFS 是可自托管的音乐聚合系统。Server 通过 Provider 接入本地/NAS 与在线平台，管理统一音乐库及聚合歌单；Web 与桌面 Client 展示并播放。桌面端优先 Tauri，提供桌面歌词、皮肤与 Windows 本地 Server 启动。
网易云、QQ 音乐是目标 Provider，并非已验证可用的接口。接入可行性、登录方式、播放与歌单能力须分别验证；平台账号权限、接口变动、付费歌曲可用性由 Provider 显式返回，不承诺全量支持。
第一版不做微服务、多语言插件运行时、双向平台歌单写回、云端账号共享或默认转码。

## 2. 仓库与协议所有权
| 仓库 | 职责 |
| --- | --- |
| MusicFS-server | 核心、Provider SDK、数据库迁移、部署、架构、OpenAPI 与事件协议 |
| MusicFS-web | 浏览器 UI、浏览器播放适配器、皮肤、消费生成的 SDK |
| MusicFS-client | Tauri 壳、桌面播放适配器、歌词窗口、启动器、桌面打包 |
Server 的 docs/contracts 后续存放 OpenAPI 与 JSON Schema，发布带版本的客户端 SDK。Web 发布可复用 UI 包，Client 固定版本依赖它，而非运行时依赖在线 Web 站点。桌面发行包内嵌 UI，避免 UI 代码在两仓库复制。
协议独立于实现语言。建议评估 Rust Server + SQLite、TypeScript UI 和 Tauri Rust 宿主；Server 语言与前端框架仍待技术验证。不采用跨编译器 C++ ABI 作为第三方插件协议。

## 3. 模块
API Gateway：鉴权、请求校验、分页、幂等、统一错误。
Library：规范化元数据、录音版本、候选关联、人工关联与禁止关联。
Playlist：本地歌单、远程快照、聚合规则、快照版本与来源追溯。
Resolver：按用户配置筛选账号权限、可用性、音质、来源优先级，创建短期播放会话。
Provider Host：能力声明、账号上下文、取消、限流、超时、错误映射。
Scheduler：NAS 扫描、远程同步、退避重试、恢复任务、并发控制。
Stream Gateway：本地文件/远程字节流转发、Range、会话鉴权、过期刷新；默认不解码。
Storage：SQLite WAL、单写入队列、封面/歌词缓存；账号密钥独立保护。
详见 [组件图](diagrams/components.puml)。

## 4. 核心实体
| 实体 | 标识与语义 |
| --- | --- |
| Recording | 内部 UUID；某个录音版本，Live/Remix/翻唱分别建模 |
| SourceTrack | providerInstanceId + externalId 唯一；某平台或本地文件的曲目 |
| RecordingSourceLink | 来源关联、匹配依据、置信度、人工/自动状态 |
| MatchExclusion | 用户禁止关联的来源对，自动匹配不可覆盖 |
| ProviderInstance | 插件类型 + 实例 + 配置；同一插件可接多个 NAS |
| ProviderAccount | userId + instanceId + accountId；密钥不出 API |
| Playlist | 用户拥有的本地/远程/聚合歌单 |
| PlaylistEntry | 稳定 entryId、顺序、来源曲目，可保留未匹配项 |
| PlaylistBinding | 聚合歌单引用的子歌单、排序、来源约束 |
| PlaylistSnapshot | revision、同步状态、生成时间、不可变条目 |
| EntryProvenance | 聚合条目对应的原歌单与原 entryId |
| PlaybackSession | 用户、设备、来源、音质、过期时间、已尝试来源 |
作品层可后续添加；不以“歌名 + 歌手”作为唯一键。时长、专辑、版本标识、ISRC 等只作证据。候选匹配不直接销毁来源记录；人工合并/解除均保留审计与可逆关联。未匹配来源仍可播放。
用户归属从第一版预留，第一版可只启用一个管理员。全局录音元数据与用户歌单、账号、播放状态分开；不能通过共享元数据推断其他用户私人文件。

## 5. 歌单聚合与同步
聚合是引用 + 可重建快照，不复制并丢弃出处。默认按 binding 顺序、子歌单原顺序合并；已确认同一 Recording 去重，未知来源按 SourceTrack 去重。去重条目保留全部出处和可选来源。
刷新先抓取完整分页到临时快照，成功后事务性发布 revision；不完整抓取不能按缺失条目删除。插件失败保留上次成功快照并标记 stale、lastSuccessAt、errorCode。
子歌单删除仅移除它的贡献；仍有其他出处的条目保留。用户可隐藏聚合条目，隐藏规则与快照独立，不修改远程歌单。本地追加条目作为独立 binding。聚合编辑默认仅更改本地规则。
远程写回以后单独设计冲突与补偿，不默认同步回所有平台。源歌单循环引用必须拒绝。
同步任务按账号/实例限流；持久化任务状态，指数退避加抖动；任务支持取消，重启后恢复。凭据失效暂停相关任务并提示登录。

## 6. Provider 协议
先交付官方受信任 Provider；第三方插件正式支持前提供进程隔离。建议 Host 与 Worker 使用版本化 JSON-RPC，经 stdio 通信；后续也可用本地 socket。不同时引入多个语言宿主。
manifest 包含 id、version、protocolVersion、capabilities、配置 schema 与权限。能力至少包括 search、metadata、playlist.read、stream.resolve、lyrics、library.scan；登录与 playlist.write 独立声明。
调用带 requestId、accountRef、deadline、cursor、取消令牌；分页结果含 nextCursor 与可用快照版本。错误码：AUTH_REQUIRED、ACCESS_DENIED、NOT_FOUND、RATE_LIMITED、TEMPORARY_FAILURE、UNSUPPORTED、UNAVAILABLE；包含 retryAfter 与可重试属性，不传出密钥。
resolve 返回 MIME、codec、质量、过期时间、Range 能力、直接播放是否允许，以及仅 Server 可见的请求头。Provider 不决定跨源匹配、去重、全局优先级。
隔离权限限定目标域名和挂载根目录，限制资源、日志长度和执行时间；进程隔离本身不是完整沙箱。官方插件也不获得无条件任意文件和网络访问。

## 7. API 草案与播放
前缀 /api/v1。错误 envelope 为 code/message/requestId/retryAfter，列表使用游标，时间使用 UTC ISO 8601。
| 路由 | 功能 |
| --- | --- |
| GET /health/live、/health/ready | 存活与就绪；就绪含 product=MusicFS、协议版本和 instanceId |
| GET /providers、/provider-instances | 能力与用户可访问实例 |
| GET /search?q=...&cursor=... | 分来源部分成功；候选关联不强制折叠 |
| GET /recordings/{id}/sources | 可选来源与用户可用性 |
| GET /playlists、/playlists/{id}/entries | 歌单与固定 revision 分页 |
| POST /playlists/{id}/bindings | 配置子歌单引用 |
| POST /sync-jobs | 异步同步，返回 jobId |
| POST /playback/sessions | recordingId/sourceTrackId、偏好、客户端能力 |
| POST /playback/sessions/{id}/fallback | 播放失败后排除已失败来源并重新解析 |
| GET /playback/sessions/{id}/stream | 会话流代理 |
| GET /events | WebSocket 任务/歌单/可用性通知 |
修改聚合规则采用 revision/If-Match 乐观锁；冲突返回 409。创建会话/任务支持 Idempotency-Key。事件含 eventId、revision、type；客户端重连后读最新快照，事件不承担唯一事实来源。

默认代理本地/NAS、需私密请求头或浏览器不兼容的在线源；直接播放仅返回无需泄露账号密钥、客户端确实可访问的 URL。会话采用短期限定 scope 的 token，不以来源 URL 作为永久播放标识。避免 token 出现在日志；CORS 使用明确 allowlist。
代理不缓冲整个文件；支持背压、取消、单 Range、206/416，保留准确 Content-Type；多 Range 不支持时明确返回错误。上游不支持 Range 时向客户端声明不能 seek，不能伪造 206。
客户端报告 codec/MIME/seek 能力。Web 用浏览器播放适配器，桌面必须验证 WebView 的 FLAC 等兼容性；不支持时采用桌面原生播放适配器，暂不强制 Server 转码。
临时 URL 失效时重新 resolve 同一来源；必要时尝试其他来源，最多 3 次不同来源，保留失败原因。只有同一录音且可 seek 的新来源才恢复原进度，否则提示从头播放。账号权限不满足与实际不可用分开显示。
歌词优先与当前来源/版本一致，来源切换后重新校验时序；可选偏移属于用户配置。
详见 [播放时序](diagrams/playback.puml)。

## 8. 部署和 Windows 生命周期
Docker：Server 独立运行，持久化 DB/cache/config，音乐目录按只读挂载；插件只在挂载根下访问。Web 可静态托管并经同源反向代理连接 Server。
本地：同一 Server 可执行程序，数据放用户数据目录。便携模式可显式指定独立目录；本地默认绑定 loopback，远程访问需要显式配置鉴权与 TLS。
Client 仅对明确配置为 managed-local 的模式自动启动；remote 模式绝不启动本机 Server。
健康检查失败不等同于进程未运行。启动器结合用户级互斥锁、受限进程身份信息、配置及端口检查；端口被其他程序占用要报错。就绪响应校验 MusicFS 身份/协议并完成授权握手，不只看 HTTP 200。
Tauri 通过受控宿主命令启动打包 sidecar，参数白名单，不向 Web UI 暴露任意命令执行。Service/external-local 模式只连接并提示状态；不自动提权启动系统服务。
等待带退避和总超时（建议 15 秒）；失败显示日志入口与重试。Client 退出默认让 Server 保持运行；明确配置随退出关闭时仅终止本客户端拥有且无其他连接的子进程。
桌面歌词独立窗口，置顶/穿透/锁定/缩放属于平台适配；Mac/Linux 支持程度待验证。皮肤限声明式资源、颜色、布局 token，初期不运行任意脚本。
详见 [启动流程](diagrams/windows-launch.puml)。

## 9. 安全和数据可靠性
所有私有歌单、文件与会话按 userId 授权。浏览器与桌面原生部分使用各自受限会话；Tauri capabilities 和 CSP 只授予需要的命令。
Server 流代理不是任意 URL 代理；只能访问 Provider 声明来源，校验重定向、DNS 与目标地址；NAS 内网访问按实例显式授权，其他网络入口默认拒绝 SSRF 目标。
密钥使用 OS key store 或外部提供主密钥，Docker 通过 secret 注入；DB 不保存明文凭据。日志脱敏 URL 参数、token、请求头与私人路径。
SQLite 迁移前做一致性备份，不能仅复制运行中的主数据库而忽略 WAL；记录 schemaVersion、备份与恢复流程。插件和元数据任务失败不能破坏已发布快照。

## 10. 原型与交付顺序
[客户端线框](prototypes/client.salt.puml) 展示导航、聚合歌单、来源状态、播放条和来源弹窗。
[连接与歌词线框](prototypes/settings.salt.puml) 展示部署模式、登录/同步状态、皮肤与歌词控制。线框使用英文标签，说明使用中文；不是可运行客户端。

M0：协议与模型评审；验证 NAS Range、浏览器/桌面格式、Tauri sidecar 与歌词窗口。
M1：本地 Provider、单用户、本地歌单、Web 播放与 Server Docker/本地运行。
M2：一个经过可行性验证的在线 Provider、导入与快照、人工关联、聚合与来源选择。
M3：桌面打包、Windows 生命周期、歌词与皮肤，再扩展第二在线 Provider。
M4：第三方插件权限隔离、多用户和远程写回按实际需求推进。

验收重点：不同录音不误合并；多出处删除正确；同步中断保留旧快照；Range/seek 正确；来源失败有界回退；凭据不传客户端；多个 Client 启动只产生一个 Server；remote 模式无本地启动。
尚未定案：Server 语言、UI 框架、原生音频库、平台 Provider 可行性、桌面各平台歌词能力。当前交付是设计与线框源码，不是实现或渲染验证完成的产品。
