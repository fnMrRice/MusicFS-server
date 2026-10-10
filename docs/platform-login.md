# 平台账号登录与客户端接口

状态：2026-10-10，已接入 Go 登录适配、Worker / Host 控制面、HTTP 和 Web 设置页；三平台授权流程有合成响应测试。网易云和 QQ 已真实取得二维码登录挑战并取消，**尚未用真实用户账号完成授权验收**。Apple 尚无实际开发者配置及用户授权验收。`account.login` 的已验证能力标记仍为 false；真实账号验收状态由登录选项的 validation 字段及本文说明。

## 接入选择

用户要求优先模拟客户端登录，并尽量调用客户端接口。实现按端点核验，不能只改 User-Agent，也不能把模拟请求成功当作账号及全部平台能力可用。

| 平台 | 本次接入 | 参考与边界 |
| --- | --- | --- |
| 网易云 | PC 设备参数及 eapi 加密 POST；获取二维码 key、轮询扫码、读取账号资料；现有搜索、资料、歌单与推荐查询统一携带账号会话走 EAPI | [api-enhanced 固定版本](https://github.com/NeteaseCloudMusicApiEnhanced/api-enhanced/tree/2db6684453297ee86dec0492842f2320b08182d9)（MIT）的 `util/crypto.js`、`util/request.js`、`module/login_qr_key.js`、`module/login_qr_check.js` 与账号查询；Go 独立实现。未实现短信/密码登录，不使用跨平台补解锁分支 |
| QQ 音乐 | 手机 QQ 扫码 → QQ 授权码 → QQLogin；账号交换、校验与曲目搜索走 musicu.fcg 的桌面 ct=19、cv=2201、设备 guid 及账号公参 | [L-1124/QQMusicApi](https://github.com/L-1124/QQMusicApi/tree/27861e51432ea6b6e88dc35c4e8c8e239c0339e8)（[AGPL-3.0](https://github.com/L-1124/QQMusicApi/blob/27861e51432ea6b6e88dc35c4e8c8e239c0339e8/LICENSE)），核查 `modules/login.py`、`core/executor.py`、`core/versioning.py` 的协议字段与流程；未引入 Python 包、服务或复制源文件。QQ扫码交互仍使用平台网页授权端点；手机 QQ 音乐原生扫码涉及 MQTT，本次未实现 |
| Apple Music | MusicKit JS 用户授权后，Go 插件携带开发者 JWT + Music-User-Token 查询本人 storefront 校验 | [Apple 用户认证](https://developer.apple.com/documentation/applemusicapi/user-authentication-for-musickit)、[MusicKit JS](https://js-cdn.music.apple.com/musickit/v3/docs/iframe.html?path=/story/get-started--page)。尚无已核验的模拟原生客户端登录实现，采用官方客户端 SDK。不是收集 Apple ID 密码；不是完整音频或 DRM 处理实现 |

三个在线源统一要求登录：实例发现、能力说明与账号控制面可以在平台未登录时使用；所有业务查询与导入/同步必须有 signedIn 账号，重启恢复的 unverified 状态也不可查询。不会退回匿名接口。网易云所有已实现业务请求改用 PC EAPI 和账号 Cookie；QQ 曲目搜索改用带 musicKey/uin 的桌面 RPC，参考原始客户端抓包说明 [QQMusicApi issue 160](https://github.com/jsososo/QQMusicApi/issues/160)，不再调用 client_search_cp。Apple Music 及 QQ 尚未实现的业务能力在登录后仍明确返回 UNSUPPORTED；不因登录成功虚构查询或完整音频能力。独立 Scraper 不获得 Provider 登录态。

当前协议修订为 online-authenticated-v3，authentication 固定为 platformAccountRequired；Server 和对应 Worker 必须同时升级，旧匿名或 v2 Worker 不兼容。v3 增加插件自己的 ID、默认名与译名声明，见 [插件身份与名称](plugin-identity.md)。Web 提供 loginRequired 来源状态及 PLATFORM_AUTH_REQUIRED（HTTP 403）提示；该错误不触发 MusicFS 会话退出。旧 Client 须同步适配这些状态，不能按旧匿名能力继续发起查询。

## 使用

构建并部署本次 Server、对应在线 Worker 和 Web 后，在设置页“音乐来源”中选择已启用实例：网易云用网易云音乐 App 扫码，QQ 音乐当前用手机 QQ 扫码。轮询至少间隔三秒，挑战有效期三分钟，可取消；失败可查看当前状态再重试，避免结果不明时重复发起。每个用户的一个实例绑定一个账号，多个账号使用多个实例。

Apple 由部署者将**自己的、适用于该网页来源的开发者 JWT**通过服务进程环境 `MUSICFS_APPLE_DEVELOPER_TOKEN` 提供；不把签名私钥或令牌放入配置提交。设置页先加载官方 MusicKit，再由用户点击“打开 Apple 授权”；授权结果只经同源 CSRF 保护的 POST 传给插件。未配置时入口明确不可用。开发者 JWT 会提供给本人页面用于 MusicKit，它不是 Apple 用户令牌。

## 会话与持久化

- 登录状态独立于 MusicFS 会话，平台授权失败不会退出 MusicFS。API 见 [OpenAPI](contracts/openapi.yaml)，账号 DTO 见 [Schema](contracts/platform-account.schema.json)，Worker 扩展见[账号控制面](contracts/plugins/v1/account.md)。目前按查询更新，没有账号事件推送。
- Host 在该实例状态目录的 `credentials/` 下保存 AES-GCM 密文和独立随机密钥；附加认证数据绑定平台、MusicFS 用户与实例。Windows 使用仅运行用户可访问的受保护 ACL；其他系统目录 0700、文件 0600。保护依赖运行账号和数据目录访问权限，不宣称是硬件密钥库。
- Worker 只经受控 stdio 接收/返回内部凭据；HTTP DTO 去除 secret。普通配置导出、诊断输出和 Git 不保存账号凭据。Web 不手工保存 Music User Token；官方 SDK 自身的令牌管理仍按其行为执行。
- 重启恢复只标记 unverified，用户点击“校验登录状态”后查询平台；业务请求检测到认证失效立即清除 Worker 中的可用会话，拒绝后续请求；账号控制面更新时同步清理持久化，旧磁盘快照即使重启恢复仍为 unverified，不能绕过校验。暂时网络失败保留当前绑定。当前不提供自动续期，过期后重新授权。
- 退出是解除本实例在 MusicFS 中的账号授权，不承诺平台所有设备退出或上游令牌撤销；Apple 页面同时调用 MusicKit unauthorize。退出先取消在途业务请求，丢弃迟到结果，后续业务访问和推荐缓存读取均拒绝；保留已导入的歌单记录。缓存按账号及随机登录代次隔离，同一账号重新登录也不复用旧缓存或游标。远程歌单导入必须显式携带当前账号 ID；旧匿名绑定须重新导入，不自动转给新账号。实例停用/离线时账号操作不可用，需恢复插件后退出。
- 现有离线备份未包含此新凭据目录；恢复备份后重新登录。不要把账号目录或密钥追加到普通制品/日志归档。后续如要备份账号授权，应单独定义密钥恢复及迁移策略。

## 验证及未验收项

合成测试覆盖 eapi 请求解码/摘要与账号 Cookie、QR 等待/已扫码/成功、QQ OAuth state 及桌面公参、MusicKit 用户令牌校验、取消后拒绝旧挑战、用户/实例隔离、CSRF、平台错误不影响 MusicFS、密文篡改拒绝、实际 Worker 重启恢复及退出持久化。回归测试还覆盖三平台全部业务入口的未登录/未校验拒绝、HTTP 403 不退出 MusicFS、EAPI 查询 Cookie、QQ 桌面搜索参数、失效后拒绝、退出取消在途查询，以及推荐缓存账号/登录代次隔离。浏览器测试覆盖未登录不查询、扫码登录后查询、退出后不再查询。

真实探测仅调用网易云、QQ 的二维码创建接口并立即取消，不输出二维码地址或平台凭据；两平台均成功。这不能代替扫码确认、账号资料读取和恢复后的真实校验。实号双账号隔离、拒绝/风控、过期、Apple 授权弹窗与令牌生命周期仍需用户在受控入口完成验收；缺少这些证据时不标为正式可用。完整音频交付和个性化歌单不在本次登录增量内。

本次查询迁移的验证为合成上游、实际 Worker/Server 进程与浏览器测试；旧匿名接口的现网成功不能作为新登录链路的现网验收。此前匿名查询现网探针已替换为默认传输的未登录拒绝检查。
