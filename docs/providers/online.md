# 在线来源接口

各平台使用的账号、目录及音频接口见[刮削与在线音乐源的接口来源](../plugins/provider-api-sources.md)。

Bilibili 已接入匿名公开资料、收藏夹/音频歌单及按需音频播放，独立范围见 [Bilibili 来源](bilibili.md)。所有来源插件必须声明 `sourceType`；在线来源声明 `network`。匿名插件仍要求 MusicFS 用户身份与实例归属验证，但不要求平台账号。以下账号流程适用于网易云、QQ 音乐和 Apple Music。

网易云、QQ 音乐和 Apple Music 由统一插件程序启动各自的独立 Worker。业务接口要求已校验的平台账号。网易云使用带会话的客户端 EAPI 查询和播放账号获准收听的完整曲目，QQ 搜索使用桌面 RPC。真实用户授权、会员音质及地区权益仍待实测。详见[账号控制面](platform-login.md)。

## 配置和实例状态

三个在线角色分别使用 `musicfs-plugins --plugin netease|qqmusic|applemusic` 启动独立进程。Windows 程序有 `.exe` 后缀。随包发现的在线插件默认启动；可通过插件或实例的 `enabled: false` 禁用：

```yaml
- pluginId: musicfs.plugin.source.netease
  executable: ../build/windows/musicfs-plugins.exe
  args: ['--plugin', 'netease']
  enabled: true
  instances:
    - instanceId: a5e47aac-f949-5e45-9709-f28a0d8443df
      enabled: true
      config: {}
```

QQ 和 Apple 对应 `musicfs.plugin.source.qqmusic`、`musicfs.plugin.source.applemusic`。平台由程序身份固定，实例不能通过请求改变平台。配置只接受空 `config`，拒绝上游 URL、代理、任意参数、Cookie、凭据、账号编号、musicRoots 和本地扫描导入选项。路径相对 YAML 所在目录，程序可配置 SHA-256；插件和实例支持动态启用，停用保存后须重启。显示名取已校验账号昵称，未登录时回退插件别名；程序路径、摘要和平台身份由部署配置管理。三平台合计最多16个在线实例，业务使用对应实例当前已校验的账号；未登录或需要手动校验时显示 loginRequired。列出实例时会自动校验已恢复的网易云账号，暂时无法校验时显示 offline，后续读取会重试。账号 ID 仅在已校验时返回。

`GET /api/v1/online-instances` 只列出本人配置实例，含 `instanceId/name/pluginId/platform/accountId/availability` 及安全能力/空配置声明。取得有效插件描述后还返回 `plugin: {id,name,localizedNames,features}`，平台标题和译名由此声明提供，不使用实例 `name` 或固定映射；字段与升级要求见 [插件身份与名称](../plugins/plugin-identity.md)。`available` 表示工作进程已通过校验、仍运行且平台账号已校验，每项能力另看 `supported`；程序丢失、禁用、摘要或握手错误则为 offline。列表不公开执行路径、运行目录、配置摘要或平台凭据。

账号歌单列表由 `playlist.list` 能力提供，`GET /online-instances/{id}/playlists` 读取对应插件当前账号的歌单。Web 在歌单管理与左侧列表中显示网易云歌单。网易云插件通过 `playlist.addTracks` 将新加入本地歌单的同来源曲目追加到关联的远程歌单。协议、分页及账号隔离见[来源歌单](../library/source-playlists.md)。

| 平台 | 当前实际范围 | 明确限制 |
| --- | --- | --- |
| 网易云 | 四类搜索、资料、推荐、歌单观测与完整曲目播放使用已验证账号的 EAPI，播放设置支持七档音质 | 可用音质由平台权限决定，试听曲目不能作为完整音轨播放。歌单列表无固定快照版本 |
| QQ | 搜索改用带账号凭据的桌面 RPC；待实号验收 | 必须登录；仍不标为已验证能力，其他资料、推荐和歌单不支持 |
| Apple Music | MusicKit 登录控制面已接入，待实号验收 | 必须登录；目录查询尚未实现，登录后返回 UNSUPPORTED；没有完整音频交付 |

各端点按能力声明使用；账号已登录不代表所有能力可用，失败不能转换成空的成功结果。

## 查询接口

以下接口均需要 MusicFS 登录 Cookie。所有 ID 是平台不透明编号，不能使用 URL 或服务器文件路径。未知实例和其他用户实例统一404。参数区分大小写，未知或重复参数、非法类型和超过限制均400。

| 方法与路径（均以 `/api/v1` 开头） | 输入 | 返回 |
| --- | --- | --- |
| GET `/online-instances/{id}/search` | `q` 必需，去两端空白后1—256 UTF-8字节；`kind=tracks\|albums\|artists\|playlists` 默认tracks；limit默认20、1—50；可选cursor | `kind`及对应数组；其他类型数组为空，另含`nextCursor/truncated/consistency/observedAt` |
| GET `/online-instances/{id}/tracks/{externalId}` | 平台曲目编号 | 标题、艺人、专辑、可空时长、平台资料页及元数据摘要 |
| GET `/online-instances/{id}/albums/{externalId}` | 平台专辑编号 | 专辑实体、歌手、可空曲目数、封面引用和来源页 |
| GET `/online-instances/{id}/artists/{externalId}` | 平台歌手编号 | 歌手实体、可空作品计数、头像引用和来源页 |
| GET `/online-instances/{id}/albums/{externalId}/tracks` | limit默认20、1—50；可选cursor | `kind=tracks`的页；游标绑定本次专辑完整观察摘要，最后一页truncated表明观察不完整 |
| GET `/online-instances/{id}/artists/{externalId}/albums` | limit默认20、1—50；可选cursor | `kind=albums`的页，保留上游专辑顺序，不固定远端快照 |
| GET `/online-instances/{id}/artists/{externalId}/tracks` | limit默认20、1—50；可选cursor | `kind=tracks`的页，固定上游hot排序，不固定远端快照 |
| POST `/online-instances/{id}/tracks/lookup` | `externalIds` 数组，1—50项；允许Origin和CSRF，无幂等键 | 保持输入顺序与重复，缺失逐项`NOT_FOUND`；只使用一次上游批量详情请求 |
| GET `/online-instances/{id}/recommendations` | limit默认20、1—50 | 公共歌单推荐、观测时间和120秒有效期；`personalized=false,nextCursor=null` |
| GET `/online-instances/{id}/daily-songs` | limit 默认 50，范围 1—50 | 当前已校验账号的每日推荐歌曲，按平台顺序返回 |
| GET `/online-instances/{id}/playlists/{externalId}` | 可访问的歌单编号，含网易云当前账号自建的私密歌单 | 条目、`complete/consistency/metadataDigest/observedAt` |
| GET `/online-instances/{id}/playlists/{externalId}/tracks` | limit默认20、1—50；可选cursor | 按位置返回来源 ID、标题、歌手、时长及可选的逐曲封面 `items[].track.coverUrl`，缺失曲目保留占位 |
| POST `/source-refs/lookup` | `sources` 是1—50项个人歌单`SourceRef`；允许Origin和CSRF，无幂等键 | 本地`SourceTrack`或远程来源及资料；顺序、重复和单项错误保留，不遍历整个曲库 |
| POST `/online-instances/{id}/tracks/{externalId}/resolve` | `{}`；另需允许Origin和CSRF，无幂等键 | 具备 `stream.resolve` 的来源返回用于播放的曲目身份；未实现的来源返回415 UNSUPPORTED，不返回上游音频地址 |

搜索游标绑定用户、实例、平台账号、登录代次、kind、规范查询和 limit；有效期5分钟，过期返回 `CURSOR_EXPIRED`，进程重启后签名失效。伪造、跨实例或改查询使用返回 `INVALID_ARGUMENT`。游标不固定远端快照；调用方须检查 `nextCursor` 和 `truncated`。歌单曲目分页另绑定本次完整条目观测摘要；观测改变须刷新首屏，不拼接新旧结果。

专辑和歌手外部身份必须与实例ID一起使用，不能仅按同名合并。歌单含可空创建者和封面；专辑含歌手、曲目数、封面，歌手含作品计数和头像。图像引用只保留插件 `resources.imageHosts` 声明主机的 HTTPS 地址，拒绝用户信息、端口、查询串和片段；Server 不会据此后台抓取文件。内置 Web 的 CSP 从受信任装配工厂的有效图片声明生成，远端 Worker 响应和 HTTP 请求不能扩大图片来源；独立部署的 Web 需同步自己的 CSP。专辑／歌手详情与关联作品的三条分页均已提供接口，仍不代表 Web 已经接入详情页面。专辑与歌单分页成员摘要忽略封面 CDN 域名轮换，避免同一观察被误判为集合变化。

来源引用批量展示先按当前用户查询，未知和其他用户来源均为`NOT_FOUND`；在线条目按实例分组，一组一次批量调用，不逐首访问上游。在线离线、禁用、限流等错误保留在对应条目，不影响本地条目。资料是当前读取值，不声称与过去的个人歌单快照具有相同修订。本人歌单`GET /playlists?q=...`同时支持Unicode小写后的名称包含匹配；过滤作用于完整本人歌单集，沿用`after` ID键集分页，实时更新可能影响后续页，改变关键词时应从第一页开始。

曲目 `metadataDigest` 是规范元数据的摘要，不能当作音频资产版本、文件 SHA-512 或识别证据。`playbackAvailability=unknown`，`sourceUrl` 是固定平台资料页。接口不返回 SourceAsset、streamUrl、请求私密头或播放SDK句柄，不把目录preview当作完整裸流。

曲目可附带展示用的 `coverUrl`。网易云从该曲目的专辑读取封面网址或图片编号，Bilibili 音频使用歌曲封面，视频优先使用分 P 首帧，再使用视频封面。缺少受信封面时省略字段。Web 可直接加载图片。插件使用 `online.TrackDigest` 计算曲目摘要，封面地址变化不会更新曲目身份或使专辑游标失效。

歌单最多10000条，按这次观测的 `position` 保留顺序和重复曲目。`complete=true` 仅说明返回数与本次申报总数相等；`consistency=unversioned` 明确没有远端固定版本。公开推荐和歌单查询不会导入个人曲库、创建媒体资产或据缺失条目删除本地数据；[显式导入和持久同步](../library/remote-playlists.md)使用独立操作。

## 资源与失败边界

当前在线协议修订为 `online-declared-v4`，要求插件明确提供 `resources`，可选 `login`；旧修订 Worker 被拒绝。平台 ID 使用有界的小写标识，外部资源 ID 使用有界、不含路径或 URL 的不透明标识。资料规则把锚定正则捕获与固定 HTTPS 主机模板绑定，Host 核对每条结果的 ID、资料 URL、图像主机、摘要和数量；歌单占位条目只按声明保留，不能成为可播放曲目。JSON Schema 约束通用结构，跨字段与插件规则由 SDK 校验。新增已知协议能力、平台 ID 或 CDN 不需要修改通用校验代码。

`login` 为空时账号选项返回 `method: none`、`configured: false`、`validation: notSupported`，Bilibili 不再显示二维码登录。其余选项通过有实例作用域的 `provider.online.accountOptions` 从插件取得，并与登录声明核对；HTTP 仍只允许实例所有者读取。界面需要实现相应登录方法才能提供交互，声明新方法不代表现有 Web 已支持该流程。

宿主同时最多4个在线查询，每次最多10秒；超额返回429和秒单位的 `retryAfter/Retry-After`，不创建无界等待。每个工作进程允许2个普通查询并发，平台网络请求共用每秒一次的准入限制。已打开的音频流独立读取和关闭，每个工作进程最多打开2条流，每条流按偏移顺序读取。上游429/业务限流会维持冷却，不隐藏重试；调用方应按提示有界重试。断连或取消传递到工作进程请求。

网络仅使用程序固定的 HTTPS 主机与路径，校验DNS公网地址、固定已校验目标连接并验证TLS，不跟随重定向、不继承代理/Cookie和账号环境。正式入口不能通过配置或RPC替换测试Transport。程序仍是管理员选择的受信任本地可执行文件，进程生命周期隔离不等于操作系统网络/文件沙箱。

离线为503 SOURCE_OFFLINE，上游异常为503 TEMPORARY_FAILURE，当前未实现能力为415 UNSUPPORTED，平台未登录或待校验为403 PLATFORM_AUTH_REQUIRED；不会退出 MusicFS 会话。公开错误不携带上游原始正文、网络地址参数或账号秘密；已有本地曲库和播放流程保持独立。

支持完整音轨播放的来源可在描述符的 `downloadQualities` 中声明供单次下载使用的音质选项及默认项。未声明时客户端可选「来源默认音质」。客户端通过 `GET /api/v1/online-instances/{id}/download-qualities` 读取可用选项，用 `POST /api/v1/online-instances/{id}/download-jobs` 提交所选曲目、音质与账号。重复提交同一请求时沿用相同的 `Idempotency-Key`。服务端返回 202，随后通过 `downloads.progress` 向本人推送进度。客户端在重连时用 `GET /api/v1/online-download-jobs/{id}?view=summary` 补读，展开明细时省略 `view` 读取每首实际保存的版本或错误。单次可传 1 至 10,000 个曲目标识，重复标识保留首次出现的位置。

按来源聚合读取、部分失败与缓存刷新见[推荐](../library/recommendations.md)。公开模型见 [OpenAPI](../contracts/openapi.yaml)、[HTTP Schema](../contracts/http.schema.json)；独立进程契约见 [online.schema.json](../contracts/plugins/v1/online.schema.json)，接入参考见[账号登录](platform-login.md)。

## 动态运行状态

停用只保存开关，当前实例继续接收查询，已准入请求与整项歌单同步照常执行；重启 Server 后才返回 `PLUGIN_DISABLED`。旧 drain/force 参数不排空或取消在线请求。配置修改仍可排空重建；同步从运行校验、上游观测到快照发布或失败记录共用一次任务租约，正常退出须回收这些资源。

重启前重新启用可撤销停用意图，不因此更换健康 Worker 或运行代次。实际启动或配置重建才产生新的运行代次，旧运行时不能借用新代次准入。保存停用不清理当前推荐缓存和共享请求；实际运行变化只清理目标实例，迟到旧结果不能覆盖新资料，重启后已禁用或进程死亡的实例不把旧成功缓存标成可用。`GET /api/v1/control` 的 `musicfs.control.v1` WebSocket 通过 Cookie/Origin 和首帧 CSRF 鉴权后提供 `capabilities.changed` 事件，首连及重连会收到当前修订，调用方据此重新读取实例和能力列表；事件本身不携带部署路径或完整配置，连接规则见[控制协议](../contracts/control-channel.md)。
