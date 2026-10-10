# 按来源读取公开推荐

S-P7-03 在[匿名在线来源](online.md)之上提供聚合读取和有界内存缓存。当前只读取公共推荐第一页，账号固定为空、`personalized=false`、`nextCursor=null`；没有账号登录或个性化推荐，也不自行生成统一推荐分数。网易公共推荐已经过正式程序的公开网络验证；QQ当前不支持推荐，Apple未接入目录授权，具体记录见[在线组合验收](validation/online-http.md)。

## 接口与来源状态

登录后调用 `GET /api/v1/recommendations?instanceId=netease-public&instanceId=qq-public&limit=20`。`instanceId` 可重复传入不同编号，最多8项，返回顺序与请求一致；省略时按本人配置顺序选择全部实例，超过8项时须显式选择。未知实例与其他用户实例均404；不能通过query传入账号、平台、URL或凭据。`limit` 默认20、范围1—50，作用于每个来源；重复编号、未知参数、非法或重复limit/refresh均400。

成功响应为 `{sources:[...]}`。每项含 `instanceId/name/platform/accountId/items/personalized/nextCursor/stale/lastSuccessAt/expiresAt/error`，保留平台推荐顺序和说明。`lastSuccessAt` 表示宿主最后一次成功读取时间，`expiresAt` 为本轮有效期上限，均为UTC。实例显示名不能用于身份；不适合公开的空白、过长或控制字符名称回退为实例编号。

一个来源失败不会遮蔽其他来源：整体仍HTTP200，调用方必须检查各项 `error`。从未成功的来源返回空items、空时间及明确错误；刷新失败则保留上次成功items/时间、标记 `stale=true` 并返回错误。它不表示推荐仍有效或曲目可以播放。当前离线实例为SOURCE_OFFLINE，QQ推荐为UNSUPPORTED，Apple为AUTH_REQUIRED；后者表示平台目录授权未实现，不能用MusicFS登录替代。

## 缓存与刷新

缓存键包含用户、实例、匿名账号、有效配置修订和limit。普通读取优先使用有效缓存；`refresh=true` 请求重新读取，但仍遵守失败冷却。过期或显式刷新失败不把上次成功列表变为空歌单。错误的 `retryAfter` 单位为秒；请求方应按来源等待，不能仅以整体HTTP状态决定重试。

生产组合使用最多64个缓存项、8 MiB预算和2个上游活动请求。上游调用最多10秒，连同等待准入的共享读取总预算最多20秒，HTTP请求取消可提前结束等待。有效期最多2分钟，同时受上游更早到期时间限制。失败默认冷却2秒，上游更长的限流提示继续生效，最长1小时。同一键并发请求共享一次读取；最后一个等待者取消后取消上游，服务关闭先取消并等待推荐请求，再关闭在线工作进程。其他在线查询仍受工作进程自身的并发、速率和固定上游限制。

缓存仅驻内存；重启不会沿用旧实例缓存，保存插件停用本身不清理当前缓存，重启后按保存开关决定可用性。推荐服务不增加新的YAML字段，使用现有在线实例。返回items时不会隐式导入个人曲库、创建音频资产、同步或删除已导入歌单，也不写回远程平台。条目的 `playlist.externalId` 配合来源 `instanceId/accountId` 可用于后续显式导入操作；打开资料与导入是不同操作。

## 当前边界

最简Web未新增推荐页面；此步通过HTTP契约与真实本地服务验收。推荐中可见的歌单和资料页不等于授权裸流，音频解析仍明确不支持。账号隔离以owner/实例/账号/配置修订共同约束；实际账号登录、凭据失效和个性化内容尚未实现，不能由匿名夹具替代验收。

公开接口见[OpenAPI](contracts/openapi.yaml)及[HTTP Schema](contracts/http.schema.json)，本轮实测与失败修复见[推荐接口验收](validation/recommendations-http.md)。
