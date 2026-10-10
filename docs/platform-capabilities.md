# 在线平台能力调查（S-P7-01）

核查日期：2026-10-10。本文是 [P7 实现计划](implementation/07-online-library.md)的调查结果，不是在线 Provider 已实现声明。当前 MusicFS 尚未实现网易云、QQ 音乐或 Apple Music Provider；本次没有新增平台运行依赖。实测请求、时间和限制见[验证记录](validation/platform-research.md)。

证据分为三类：**官方文档**描述平台公开契约；**参考源码**说明指定提交如何调用接口，不代表平台承诺或当前可用；**匿名实测**仅证明记录中的一次请求结果。网易云与 QQ 的参考仓库是各项目的官方仓库，并非音乐平台发布的官方 SDK。

## 1. 固定参考版本与用途

| 参考项目 | 本次固定版本 | 提交时间（UTC）/ 仓库状态 | 许可与本期用途 |
| --- | --- | --- | --- |
| [NeteaseCloudMusicApiEnhanced/api-enhanced](https://github.com/NeteaseCloudMusicApiEnhanced/api-enhanced) | `v4.41.1` / `2db6684453297ee86dec0492842f2320b08182d9`（当时 main） | 2026-10-05；未归档 | [MIT](https://github.com/NeteaseCloudMusicApiEnhanced/api-enhanced/blob/2db6684453297ee86dec0492842f2320b08182d9/LICENSE)；参考搜索、资料、公开推荐和歌单请求 |
| [jsososo/QQMusicApi](https://github.com/jsososo/QQMusicApi) | `13b08afd3180cc74d76fff208956b77a560abd22`（当时 master；标签查询为空） | 2022-07-09；未归档 | [GPL v3](https://github.com/jsososo/QQMusicApi/blob/13b08afd3180cc74d76fff208956b77a560abd22/LICENSE)；调查字段、账号边界与历史请求，不直接纳入源码 |
| [zachomedia/apple-music-webplayer](https://github.com/zachomedia/apple-music-webplayer) | `87dbad9984691b927e5c13ca53006372a4257cdd`（当时 master；标签查询为空） | 2021-03-26；未归档 | [MIT](https://github.com/zachomedia/apple-music-webplayer/blob/87dbad9984691b927e5c13ca53006372a4257cdd/LICENSE)；仅参考 MusicKit Web 的授权与播放器分工 |

Apple 的能力判断以 [Apple Music API](https://developer.apple.com/documentation/applemusicapi)和 [MusicKit](https://developer.apple.com/musickit/)官方文档为准。上表日期是提交时间，不是平台接口最近验证时间；未归档也不等于仍在维护。项目许可不代表依赖许可、平台内容授权或账号权限；后续若复制、改写或分发参考实现，仍须核对相应许可，换语言不消除原有义务。

## 2. 能力矩阵

| 能力 | 网易云音乐 | QQ 音乐 | Apple Music |
| --- | --- | --- | --- |
| 不带账号的搜索 | **匿名实测**：旧 `/api/search/get` 单曲搜索一次成功；不证明参考项目加密请求链或其他搜索类型可用 | **匿名实测**：`client_search_cp` 单曲搜索一次成功；不证明歌单/歌手等类型可用 | **官方文档 + 匿名实测**：目录也需要开发者令牌；本次无令牌请求为 401，未完成目录查询 |
| 账号授权 | **参考源码**有二维码、短信等入口和登录状态查询；未登录实测，暂不宣告支持 | **参考源码**接收网页版 Cookie，QQ/微信账号形态不同；未验证正式授权、刷新或隔离 | **官方文档**：目录使用开发者令牌；个人数据额外需要用户授权及 Music User Token；本次均无凭据验证 |
| 曲目资料 | **参考源码**有按 ID 查询详情；标题、时长、平台质量字段仍需真实响应验证 | **参考源码**有 `get_song_detail_yqq`；必须区别歌曲 ID、songmid 与媒体 ID | **官方文档**有目录资料、时长、ISRC 等字段；目录 ID 与用户曲库 ID 分离；尚未授权实测 |
| 歌单读取 | **参考源码**有详情、trackIds 和分页补详情；匿名返回是否完整待验证 | **参考源码**有公开歌单详情及用户歌单；私人/收藏入口和空结果不能按公开列表推断 | **官方文档**区分目录歌单与个人曲库歌单，后者需用户令牌；均未授权实测 |
| 公开推荐 / 榜单 | **参考源码**：`personalized` 是候选入口；仅有 limit，当前实现无 offset；未探测 | **参考源码**有分类歌单、热门推荐；接口名“为你推荐”不能证明个性化；未探测 | **官方文档**提供目录 charts；无需用户账号不等于无需开发者令牌；未实测 |
| 个性化推荐 | **参考源码**有每日推荐入口，需与公开推荐分开；无账号未验证 | **参考源码**的 daily 使用当前账号 Cookie 并解析页面；无账号未验证 | **官方文档**提供用户推荐，属于个人授权范围；无用户令牌未验证 |
| 音质 / 权限 | 平台 level、权限、试听信息只是来源申报；未验证对应账号的完整播放资格或实际编码 | 参考实现有 m4a、128、320、ape、flac 参数；文件名前缀不证明实际质量或账号权限 | `audioVariants`、`playParams`、`previews` 含义不同；目录标记不证明当前账号可完整播放 |
| 完整音频交付 | **未验证**：未调用 URL resolver，未下载音频；暂无 MusicFS `stream.resolve` 能力 | **未验证**：未请求 vkey/purl 或音频；暂无 MusicFS `stream.resolve` 能力 | 官方 MusicKit 支持授权播放，但未见当前 Server 所需完整裸流契约；本期设计应明确 `stream.resolve=UNSUPPORTED` |
| 刷新 / 限流 | 参考文档称登录刷新不适用于二维码 Cookie；提示高频可能触发风控/503；无固定 QPS 保证，均未实测 | 参考文档刷新仅限 QQ；实现注释称微信未试；无固定配额或刷新有效期保证，未实测 | 开发者 JWT 最长约六个月，MusicKit 管理用户令牌；按开发者令牌限流并可能 429，未公开本次可用固定 QPS，未实测 |
| 分页 / 版本 | 搜索 offset/limit；track/all 每次重新读 trackIds 后切片，不能证明跨页快照一致 | pageNo/pageSize 或 sin/ein；参考实现未提供强一致快照保证 | 集合与关系各自有 `next`/offset；lastModifiedDate 不构成分页一致承诺，本次未核到固定快照保证 |

矩阵中的“未验证”不意味着平台必然不支持；意味着 MusicFS 不能据此把该能力标为 supported。搜索结果总数不证明已读取全部结果，推荐项可见也不证明曲目可播放。

## 3. 平台细节与实现取舍

### 网易云

参考 [search](https://github.com/NeteaseCloudMusicApiEnhanced/api-enhanced/blob/2db6684453297ee86dec0492842f2320b08182d9/module/search.js)、[song_detail](https://github.com/NeteaseCloudMusicApiEnhanced/api-enhanced/blob/2db6684453297ee86dec0492842f2320b08182d9/module/song_detail.js)、[playlist_track_all](https://github.com/NeteaseCloudMusicApiEnhanced/api-enhanced/blob/2db6684453297ee86dec0492842f2320b08182d9/module/playlist_track_all.js)：分别有搜索分页、批量详情、先取 ID 再分批补资料的实现。批量 1000 条是参考源码注释/默认值，不能当成平台永久上限。歌单顺序与重复出现的同一曲目都应保留，不能只用曲目 ID 集合替代歌单条目。

[personalized](https://github.com/NeteaseCloudMusicApiEnhanced/api-enhanced/blob/2db6684453297ee86dec0492842f2320b08182d9/module/personalized.js)和 [recommend_resource](https://github.com/NeteaseCloudMusicApiEnhanced/api-enhanced/blob/2db6684453297ee86dec0492842f2320b08182d9/module/recommend_resource.js)是不同入口。前者目前注释掉 offset，适配器不能伪造下一页；后者须按账号作用域验证。登录刷新和高频限制依据[对应版本说明](https://github.com/NeteaseCloudMusicApiEnhanced/api-enhanced/blob/2db6684453297ee86dec0492842f2320b08182d9/public/docs/home.md)，本次没有执行登录或刷新。

[song_url_v1](https://github.com/NeteaseCloudMusicApiEnhanced/api-enhanced/blob/2db6684453297ee86dec0492842f2320b08182d9/module/song_url_v1.js)包含可调用其他来源的 unblock 分支，甚至自行拼装 type、level、试听字段。**本项目不采用 unblock、补解锁、跨平台替换、借用账号或伪装地区的路径**；本轮只把资料、公开推荐和公开歌单列为首批适配候选。将来完整播放须逐账号验证原平台授权结果、到期、试听范围和实际编码，不能沿用该分支的申报值。

### QQ 音乐

参考 [search](https://github.com/jsososo/QQMusicApi/blob/13b08afd3180cc74d76fff208956b77a560abd22/routes/search.js)、[song](https://github.com/jsososo/QQMusicApi/blob/13b08afd3180cc74d76fff208956b77a560abd22/routes/song.js)、[songlist](https://github.com/jsososo/QQMusicApi/blob/13b08afd3180cc74d76fff208956b77a560abd22/routes/songlist.js)、[recommend](https://github.com/jsososo/QQMusicApi/blob/13b08afd3180cc74d76fff208956b77a560abd22/routes/recommend.js)。匿名成功只覆盖单曲搜索的一个旧入口；2022 年的资料、推荐和歌单路径要逐个重新验收，不能凭仓库存在就批量启用。

播放参考代码使用全局 Cookie、可切换账号参数和未完整绑定账号的 URL 缓存键；这些设计不进入 MusicFS。任何未来缓存、任务、凭据、私密头及资源引用都须绑定 user/instance/account，公开搜索不自动借用管理员账号。现有 [user](https://github.com/jsososo/QQMusicApi/blob/13b08afd3180cc74d76fff208956b77a560abd22/routes/user.js)与[说明](https://github.com/jsososo/QQMusicApi/blob/13b08afd3180cc74d76fff208956b77a560abd22/docs/README.md)仅作为待验证行为清单；本次没有使用参考项目的演示服务、Cookie 共享或获取 Cookie 的插件。

### Apple Music

目录请求需要开发者 JWT，ES256 签名、有效期受限；个人库需要额外 Music User Token，Web 上由 MusicKit 管理用户授权。这些是官方能力声明，本次没有导入密钥、生成令牌或进行用户登录。[开发者令牌](https://developer.apple.com/documentation/applemusicapi/generating-developer-tokens)、[用户认证](https://developer.apple.com/documentation/applemusicapi/user-authentication-for-musickit)。

目录请求带 storefront，个人曲库另有 ID；移除后重新添加可能产生新的曲库 ID。适配缓存需绑定地区、语言和账号，不能把个人 ID 当通用目录 ID。[请求与响应](https://developer.apple.com/documentation/applemusicapi/handling-requests-and-responses)。

`durationInMillis`、ISRC、`audioVariants` 是资料；`playParams` 用于启动播放，`previews` 是试听资产，分享 `url` 也不是音频地址。目录的无损标记不能当 Server 已获 FLAC 字节或可执行 DSP 的证据。[Songs.Attributes](https://developer.apple.com/documentation/applemusicapi/songs/attributes-data.dictionary)。

MusicKit Web 提供浏览器内播放；参考播放器也调用 `MusicKit.configure`、授权状态、DRM 能力和播放器队列。首期可在真实凭据验证后仅实现目录/歌单能力；如以后采用 MusicKit Web，需另行设计播放契约与 Web 适配，不能给现有 Gateway 填造裸流地址或新的 deliveryMode。[官方 MusicKit](https://developer.apple.com/musickit/)、[固定版本播放器](https://github.com/zachomedia/apple-music-webplayer/blob/87dbad9984691b927e5c13ca53006372a4257cdd/src/store/modules/musicKit.js)。

分页跟随平台相对 `next`，且关系对象也可能独立分页；宿主仍须限制合法平台路径和请求预算。当前 [Playlists.Attributes](https://developer.apple.com/documentation/applemusicapi/playlists/attributes-data.dictionary)有可选 lastModifiedDate；本次该模型未见 versionHash 字段，也未核到把修改日期用作强一致分页快照的承诺。[分页文档](https://developer.apple.com/documentation/applemusicapi/fetching-resources-by-page)。

## 4. 建议的后续实施顺序（尚未实施）

1. 先做 Go 独立网易云 Provider 的公开搜索、曲目资料、公开推荐和显式公开歌单读取；每个入口先有公开实测和脱敏契约，失败能力保持不可用。公开推荐不带账号兜底，歌单仅由用户明确导入时入库。
2. QQ 先落实已探测搜索类型，再逐一验证资料和公开歌单/分类推荐。保留平台 ID 的字符串表示；不移植全局 Cookie 或跨账号 URL 缓存。
3. 用户具备自身 Apple 开发者授权后，验证指定 storefront 的目录和歌单；个人库与个性推荐另做用户授权验收。完整播放保持不支持，除非另有可执行、已验证的交付设计。
4. 最后进入各平台账号、刷新与音频资源验收。分别验证订阅/地区/试听/过期，不把匿名搜索成功升级成完整播放支持。没有原平台授权的候选不进入跨来源恢复。

所有 Go 适配都独立构建，不隐式启动 Node/Python 服务、不安装参考项目依赖。若未来改用服务包装，应显式修改部署和许可说明并重新验证，不作为本调查的既定实现。

三个平台目前都按“未证明跨页一致”设计同步：有界读取并暂存、检查重复页/循环 cursor/半途失败，只在有效任务代次下发布。缺失项先记 suspectedRemoved，经后续完整同步或独立查询复核后再移除来源贡献；失败、权限收缩与推荐列表变化都不直接删除既有导入内容。这是 P7-04 待实现要求，不是当前已有功能。

## 5. 真实授权验收需要的输入

本轮不要求提供凭据。以后开展相应验收时，由用户选择专用测试账号、实际地区/订阅状态、允许读取的测试歌单与曲目，并通过本机受控授权入口确认；Cookie、密码、私钥和可复用授权 URL 不贴进对话、日志或仓库。

网易云需确认所选登录方式的官方交互及刷新限制；QQ 需分别确认 QQ 或微信账号的授权形态。Apple 需用户自己的 MusicKit 开发者配置（Team ID、Key ID、媒体标识、允许来源、storefront/语言），私钥仅在本机受控位置使用；个人库还需 MusicKit 用户同意，完整播放还须验证相应订阅和客户端能力。不能从第三方演示站借令牌。

账号隔离验收至少覆盖两个独立测试账号/实例以及无授权调用；仅有一个真实账号时，另一个合成账号能验证本地隔离逻辑，但不能冒充平台双账号实测。可采用的脱敏 fixture 与待验收项目见[验证记录](validation/platform-research.md)。
