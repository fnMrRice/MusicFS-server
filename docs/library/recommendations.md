# 按来源读取推荐

[在线来源](../providers/online.md)提供按实例聚合读取和有界内存缓存。三平台业务都要求当前实例具有已校验账号；“公共推荐”描述内容范围，不表示允许匿名访问。网易云适配公共推荐第一页，`personalized=false`、`nextCursor=null`；QQ 和 Apple Music 尚未实现推荐。账号链路仍待[真实授权验收](../providers/platform-login.md#未验收范围)。

## 接口与来源状态

调用 `GET /api/v1/recommendations?instanceId={实例UUID}&limit=20`。`instanceId` 可重复传入不同编号，最多8项，返回顺序与请求一致；省略时按本人配置顺序选择全部实例，超过8项时须显式选择。未知实例与其他用户实例均404；不能通过 query 传入账号、平台、URL或凭据。`limit` 默认20、范围1—50，作用于每个来源；重复编号、未知参数、非法或重复 limit/refresh 均400。

成功响应为 `{sources:[...]}`。每项含 `instanceId/name/platform/accountId/items/personalized/nextCursor/stale/lastSuccessAt/expiresAt/error`，保留平台推荐顺序和说明。`lastSuccessAt` 表示宿主最后一次成功读取时间，`expiresAt` 为本轮有效期上限，均为UTC。实例显示名不能用于身份，来源名称按插件声明显示。

一个来源失败不会遮蔽其他来源：整体仍 HTTP 200，调用方必须检查各项 `error`。从未成功的来源返回空 items、空时间及明确错误；暂时刷新失败可保留同一账号范围的成功结果并标记 `stale=true`。未登录、账号失效或权限拒绝时不返回旧缓存。离线为 `SOURCE_OFFLINE`，平台账号未校验为 `PLATFORM_AUTH_REQUIRED`，已登录但能力未实现为 `UNSUPPORTED`；这些状态均不表示曲目可播放。

## 缓存与刷新

缓存键包含用户、实例、平台账号、登录代次、有效配置修订和 limit；读取前后均检查账号仍有效。同一账号重新登录也不复用旧缓存。普通读取优先使用有效缓存；`refresh=true` 请求重新读取，但仍遵守失败冷却。错误的 `retryAfter` 单位为秒，请求方按来源等待。

生产组合使用最多64个缓存项、8 MiB预算和2个上游活动请求。上游调用最多10秒，连同等待准入的共享读取总预算最多20秒，HTTP请求取消可提前结束等待。有效期最多2分钟，同时受上游更早到期时间限制。失败默认冷却2秒，上游更长的限流提示继续生效，最长1小时。同一键并发请求共享一次读取；最后一个等待者取消后取消上游，服务关闭先取消并等待推荐请求，再关闭在线工作进程。其他在线查询仍受工作进程自身的并发、速率和固定上游限制。

缓存仅驻内存；重启不会沿用旧实例缓存，保存插件停用本身不清理当前缓存，重启后按保存开关决定可用性。推荐服务不增加新的YAML字段，使用现有在线实例。返回items时不会隐式导入个人曲库、创建音频资产、同步或删除已导入歌单，也不写回远程平台。条目的 `playlist.externalId` 配合来源 `instanceId/accountId` 可用于后续显式导入操作；打开资料与导入是不同操作。

## 当前边界

推荐可见不代表曲目可播放，音频解析仍不支持。个性化推荐尚未实现；真实账号授权、失效及双账号隔离尚未完成平台验收，合成响应不能代替实号证据。

公开接口见 [OpenAPI](../contracts/openapi.yaml)及 [HTTP Schema](../contracts/http.schema.json)。
