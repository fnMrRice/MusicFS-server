# S-P7-02：Go 公开元数据适配核心验证

核查日期：2026-10-10。本文保留 `src/online` 核心、独立Worker与宿主接口的首轮验证范围，当时尚未接入Server配置和HTTP。后续组合已完成，见[在线宿主验收](online-http.md)；显式歌单入库另见[远程同步](remote-playlists-http.md)。不能用本核心记录认定整个P7完成。前期调查见[平台矩阵](../platform-capabilities.md)与[调查记录](platform-research.md)，没有新增Go或Node/Python运行依赖。

## 本轮范围与接口

- `New(platform, Scope, Options)`：宿主绑定 `UserID/InstanceID/AccountRef`；本期拒绝非空 `AccountRef`，不会把账号请求静默变成匿名请求。宿主须为同一作用域复用一个客户端，而非逐请求重建绕过限流。
- `Search`：网易云与 QQ 的匿名单曲搜索映射；页长 1–50、最多观测 10000 条，截断显式返回 `truncated`。游标 HMAC 绑定平台、用户、实例、账号、查询和页长，5 分钟过期，客户端重建后失效。游标表示继续观察，不代表上游强快照。
- `Metadata`：本期只有网易云单曲资料。平台 ID 保留十进制字符串，超过 JavaScript 安全整数不会舍入；时长未知为 `null`。
- `PublicRecommendations`：只有已实测的网易云匿名首屏，保留平台顺序和推荐文字，不发明评分或 offset。`personalized=false`，本地建议有效期 2 分钟，不代表平台保证。
- `ObservePlaylist`：只读取网易云公开歌单的有序曲目 ID，最多 10000 个；相同歌曲重复出现仍保留每个位置。位置不是稳定远程 entry ID。只在 `trackIds` 数量与 `trackCount` 一致时标 `complete=true`，任何响应都仍为 `consistency=unversioned`。失败由调用层保留旧快照；本包没有删除旧曲库的权限。
- `Track.MetadataDigest` / `PlaylistObservation.MetadataDigest`：本地归一化元数据的 SHA-256 摘要，用于检测资料变化，绝不是音频版本、完整文件 hash 或时间轴证据。
- `SourceURL` 只生成固定平台的资料页 URL，不返回音频地址。`PlaybackAvailability=unknown`；不把权限位、申报码率或元数据存在解释为整曲可播。三家 `Resolve` 都明确返回 `UNSUPPORTED`。
- Apple 目录操作返回 `AUTH_REQUIRED`，说明需要尚未接入的 developer token；未实现登录、私人歌单、刷新、订阅/地域权益判断、MusicKit 播放或完整裸流。

## HTTP 边界

生产默认仅固定 HTTPS 主机 `music.163.com`、`c.y.qq.com` 和代码内的具体路径；外部 ID 不能成为 URL。DNS 结果先检查公网地址，再连接已检查的地址，TLS 验证原主机。禁用代理、Cookie 容器和自动重定向，不接受用户提供的私密请求头。`Options.Transport` 仅是可信宿主/测试注入点，不得直接暴露为用户配置。

请求默认 10 秒，允许宿主设置 1–30 秒；响应正文最多 2 MiB，响应头最多 32 KiB。每个客户端最多每秒准入一次，不自动重试；HTTP 429 使用 `Retry-After` 秒数或日期，冷却上限 1 小时，无有效值为 60 秒，业务码 429/503 同样冷却 60 秒。取消会传到 HTTP 请求。公开错误不含上游正文、Cookie、URL 参数或底层网络异常细节。401/403/404/429/服务失败分别映射，不能把上游出错当空歌单。

## 实际匿名验证

请求只读取公开资料，无 Cookie、Authorization、音频下载、登录、私人歌单或平台写入。查询 `MusicFS`，搜索和推荐每次仅 1 条。原始响应与探测工具仅留忽略目录 `build/_online-research/p7-core`；Git 记录汇总，不纳入真实曲目标题/ID、账号资料或媒体授权地址。

| 时间（UTC） | 实际程序 / 请求 | 结果 |
| --- | --- | --- |
| 04:20:33–44 | 独立匿名研究请求：网易搜索、单曲详情、公开推荐、推荐项歌单 | 均 HTTP 200 / 业务码 200；歌单请求 `n=1` 返回 1 个详情和 565 个曲目 ID，数量与申报总数一致 |
| 04:20:41 | 独立 QQ 匿名单曲搜索 | HTTP 500，空正文；与 P7-01 当日 01:24 的一次成功并列记录，不隐藏现网不稳定 |
| 04:27:51–55 | **生产 `src/online` 默认 Transport**：网易搜索、资料、推荐、歌单 | 四项成功：搜索 1 条；资料 ID 一致且时长可得；匿名推荐 1 条；歌单 95 个条目、`complete=true`、`unversioned` |
| 04:28:00 | **生产 `src/online` 默认 Transport**：QQ 搜索 | 正确返回 `TEMPORARY_FAILURE`；没有完整成功结果，QQ `search.tracks` 本轮不宣告 supported |
| 04:46:38–42 | **生产 `pluginhost.StartOnline` + 独立 `musicfs-plugin-netease` 进程**：搜索、资料、推荐、歌单 | 四项成功：搜索 1 条、资料 ID 一致、推荐 1 项、歌单 209 条且 `complete=true/unversioned`；随后关闭进程成功 |

各次推荐返回了不同公开歌单，565、95、209 是各次独立观测数量，不是同一歌单的变更证明。未压力测试平台限额，未验证分页过程中远端增删。网易四项 `supported` 仅指上述具体适配能力；上游接口仍可能变化，不能保证持续可用。

## 独立 Worker、SDK 与宿主

三个发布入口为 `src/app/musicfs-plugin-netease`、`musicfs-plugin-qqmusic`、`musicfs-plugin-applemusic`，固定身份 `musicfs.netease`、`musicfs.qqmusic`、`musicfs.applemusic`。公共实现位于 `src/plugins/provideronline`，每个进程固定一个平台、用户和实例，匿名 `AccountRef` 为空。平台不能由业务请求切换。Worker 仅接收管理员选定的受信任可执行文件；进程和 Job/进程组不代表操作系统网络或文件沙箱。

`pluginhost.StartOnline(ctx, OnlineConfig)` 校验可执行文件 SHA-256 后，在私有临时工作目录启动进程；不传继承 Cookie、代理、账户或工具环境。`OnlineConfig` 包含 `Executable/SHA256/StateDir/Platform/UserID/InstanceID/Disabled/Timeout`。禁用在创建目录与启动前拒绝。宿主验证 nonce、插件身份、版本、平台/架构、能力及空配置 descriptor。`OnlineWorker` 提供 `Search/Metadata/PublicRecommendations/ObservePlaylist/Resolve/Describe/Descriptor/Close/Done/Platform`，业务请求不能覆盖宿主注入的作用域。

新 SDK 在 `src/pluginproto/online.go`，独立契约为 [online.schema.json](../contracts/plugins/v1/online.schema.json)，合成请求样本为 [online-search.json](../contracts/plugins/v1/fixtures/online-search.json)。使用既有 JSON-RPC 2.0 / Content-Length / `$/cancelRequest`；只新增 `provider.online.describe/search/metadata/playlist/recommendations/resolve` 分支，未修改既有本地扫描 DTO 或 HTTP 协议。每次请求有随机 task ID、用户/实例/账号及 1–30000 毫秒预算，响应 `{context,value}` 必须原样绑定。未知字段、跨作用域结果、错误来源页、摘要或顺序均被拒绝。

Manifest 仅声明已验收能力：网易四项；QQ/Apple 列表为空，但 descriptor 仍说明各能力为何不可用。Apple 搜索真实通过 Worker 返回 `AUTH_REQUIRED`；QQ 未验收资料返回 `UNSUPPORTED`，网络失败保留 `TEMPORARY_FAILURE`。三家音频解析均明确不支持。现有 `Manifest.Configuration` 是本地导入选项模型，因此保持 `null`；在线 descriptor 独立声明 `online-anonymous-v1`、`config:{}`，不接受 URL、Cookie、developer token 或任意扩展配置。

同一 Worker 复用 HTTP 客户端、限流状态和游标密钥；同时最多 2 个请求。结果超过控制帧预算时返回 `RATE_LIMITED`，不使控制通道损坏。取消传到 HTTP context；Windows 使用 Job 对象、Linux 使用进程组回收，关闭最多等待 3 秒。宿主关闭后等待进程退出并移除空工作目录。SDK 的 `Serve` transport 参数仅用于可信进程内测试，正式三个入口均固定默认网络实现，没有环境变量或 RPC 可开启任意目标地址。

## 可复现的离线检查

`tests/unit/online` 使用明确合成的响应及本地 TLS HTTP 服务，不包含商业音频或真实账户。覆盖：ID 精度、未知时长、来源页与音频分离、搜索游标作用域/篡改/分页、取消、并发准入、HTTP/业务限流冷却、错误正文不泄露、重定向拒绝、2 MiB 上限、坏 JSON、缺失字段、资料 ID 不符、重复歌单条目、完整/部分观测、公开/非公开歌单、匿名推荐和 Apple/账号未支持状态。

最终 13 项测试：Windows `go test -mod=readonly -race ./tests/unit/online` 通过（7.519 秒）；WSL Arch / Linux 同命令通过（2.138 秒），临时文件使用原生 `/tmp/musicfs-online-validation`。`go vet -mod=readonly ./src/online ./tests/unit/online` 与限定新文件的 `git diff --check` 通过。生产源文件各 86–161 行。实际远端请求只在 Windows 执行，Linux 验证为本地 TLS/合成响应测试；两者不互相替代。

Worker 接入后的最终定向 race：Windows 核心 2.678 秒、Worker 单元 7.198 秒、契约 2.344 秒、真实进程集成 18.947 秒；Linux 对应 2.142、1.171、1.856、61.812 秒，均通过。单元新增 3 项（握手/作用域、取消、超帧结果），契约新增 2 项（实际 DTO 与禁用能力），真实进程新增 4 项（完整映射/重复条目/回收、取消/恶意响应、三个正式入口、禁用/SHA/QQ 500 映射）；Linux 集成时间包含在 WSL 构建所有测试/正式入口。命令为 `go test -mod=readonly -race ./tests/unit/online ./tests/unit/plugins/provideronline`，以及 `go test -mod=readonly -race ./tests/contract/plugins ./tests/integration/plugins -run 'TestOnline|TestActualOnline' -count=1`。包含宿主/SDK/Worker 的 `go vet` 通过。新增生产文件最多 163 行；未宣称整个仓库已在本子步骤重新全量回归。

## 一手参考、版本与许可

1. 网易云参照 [api-enhanced v4.41.1 固定提交的 search.js](https://github.com/NeteaseCloudMusicApiEnhanced/api-enhanced/blob/2db6684453297ee86dec0492842f2320b08182d9/module/search.js)、[song_detail.js](https://github.com/NeteaseCloudMusicApiEnhanced/api-enhanced/blob/2db6684453297ee86dec0492842f2320b08182d9/module/song_detail.js)、[personalized.js](https://github.com/NeteaseCloudMusicApiEnhanced/api-enhanced/blob/2db6684453297ee86dec0492842f2320b08182d9/module/personalized.js)、[playlist_detail.js](https://github.com/NeteaseCloudMusicApiEnhanced/api-enhanced/blob/2db6684453297ee86dec0492842f2320b08182d9/module/playlist_detail.js)。提交 `2db6684453297ee86dec0492842f2320b08182d9`，许可 [MIT](https://github.com/NeteaseCloudMusicApiEnhanced/api-enhanced/blob/2db6684453297ee86dec0492842f2320b08182d9/LICENSE)。本轮再次实际从官方 raw 地址读取 search.js，HTTP 200 / 678 字节 / SHA-256 `0272ab7a5620253eefc80708f6116876e8ae0c3745d1028a0062d71d8f689bf4`；其余固定源码已在 P7-01 下载核对。本实现独立编写 Go 请求/映射，未引入该 Node 项目、签名/解密模块、unblock、换源或模拟地区能力。
2. QQ 参照 [QQMusicApi 固定提交的 search.js](https://github.com/jsososo/QQMusicApi/blob/13b08afd3180cc74d76fff208956b77a560abd22/routes/search.js) 的端点/参数线索，实际浏览了官方 raw 文件；提交 `13b08afd3180cc74d76fff208956b77a560abd22`，许可 [GPL-3.0](https://github.com/jsososo/QQMusicApi/blob/13b08afd3180cc74d76fff208956b77a560abd22/LICENSE)。只作为接口行为调查来源；没有复制或移植其程序实现、缓存、Cookie 或账号代码，Go 映射独立按匿名响应字段编写。旧参考中的 HTTP 地址改为本期实际探测的 HTTPS，未引入 GPL 依赖。其元数据/歌单/音频入口没有通过本轮现场验证，未纳入 supported。
3. Apple 依据官方 [开发者 token](https://developer.apple.com/documentation/applemusicapi/generating-developer-tokens)、[用户授权](https://developer.apple.com/documentation/applemusicapi/user-authentication-for-musickit)、[歌曲属性](https://developer.apple.com/documentation/applemusicapi/songs/attributes-data.dictionary)、[分页](https://developer.apple.com/documentation/applemusicapi/fetching-resources-by-page)。官方 HTML 引导至 Markdown/DocC，P7-01 已保存并核对；本轮再次确认官方入口。目录请求需要开发者 token，个人资源还需用户授权；preview/playParams 不是 Server 整曲授权字节。没有用户凭据，授权调用未验证。

后续宿主接入仍需独立处理账号秘密存储、缓存身份、持久快照/代次和保守移除。没有足够授权实测前，不应开启账号或音频能力。推荐展示不会自动导入个人曲库，导入/绑定另由明确的用户动作触发。
