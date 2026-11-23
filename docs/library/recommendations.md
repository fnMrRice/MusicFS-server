# 按来源读取推荐

推荐页展示来源提供的每日推荐歌曲和推荐歌单。网易云使用当前实例已校验的平台账号读取当天的歌曲，也支持公共推荐歌单。QQ 和 Apple Music 暂未实现推荐。

## 每日推荐歌曲

声明 `recommendations.tracks` 的实例通过 `GET /api/v1/online-instances/{id}/daily-songs` 返回当前账号的每日推荐歌曲。`limit` 默认为 50，取值 1 至 50。`tracks` 保留平台顺序，`observedAt` 是读取时间。Web 在当前推荐页展示歌曲。推荐歌单的历史页只展示当时保存的歌单。

网易云插件通过已校验的账号会话读取 `/api/v3/discovery/recommend/songs` 的 `data.dailySongs`。接口依据见 [api-enhanced 的 recommend_songs.js](https://github.com/NeteaseCloudMusicApiEnhanced/api-enhanced/blob/2db6684453297ee86dec0492842f2320b08182d9/module/recommend_songs.js)。

## 接口与来源状态

调用 `GET /api/v1/recommendations`。以下查询参数均可省略。

| 参数 | 含义 |
| --- | --- |
| `instanceId` | 可重复传入不同实例编号，最多 8 个，按请求顺序返回。省略时使用本人配置的顺序；已配置超过 8 个实例时须显式选择。 |
| `limit` | 每个来源的推荐数量，默认 20，取值 1 至 50。 |
| `refresh` | `true` 表示手动刷新，默认 `false`。 |
| `history` | 默认 0 表示当前推荐，1 表示上一轮，最多回看 5 轮。读取历史时不能指定 `refresh=true`。 |

成功响应为 `{sources: [...], historyDepth: 2}`。例如 `historyDepth=2` 表示可以请求 `history=1` 或 `history=2`。未保存的轮次、未知实例及其他用户的实例返回 404。重复编号、未知参数、非法参数或重复的 `limit`、`refresh`、`history` 返回 400。

每个来源保留平台给出的歌单顺序、推荐说明与可选封面。`lastSuccessAt` 表示宿主最后一次成功读取来源的时间，`expiresAt` 表示该次来源观测的有效期；两者均为 UTC 时间戳。页面每日更新的判断由服务端完成。

一个来源失败不会遮蔽其他来源：整体仍 HTTP 200，调用方必须检查各项 `error`。从未成功的来源返回空 items、空时间及明确错误；暂时刷新失败可保留同一账号范围的成功结果并标记 `stale=true`。未登录、账号失效或权限拒绝时不返回旧缓存。离线为 `SOURCE_OFFLINE`，平台账号未校验为 `PLATFORM_AUTH_REQUIRED`，已登录但能力未实现为 `UNSUPPORTED`；这些状态均不表示曲目可播放。

## 每日更新与手动刷新

服务端按自身时区的自然日判断是否刷新。每天首次打开推荐页时读取平台，其余时间再次打开时读取已保存的推荐。服务器跨过午夜后，下次打开才触发当天更新。客户端无需提供本机时间、时区或日期。

手动刷新使用 `refresh=true`，可在当天再次读取。服务端仍遵守来源限流和失败冷却，`error.retryAfter` 以秒为单位。同一来源的并发刷新共享上游请求。当天自动刷新失败后再次打开会显示上次尝试的结果，可手动重试。

至少一个来源成功时，服务端保存这一轮的完整页面，把原先的页面加入历史。全部来源失败时，服务端记录这次尝试的状态，保留已有历史。时光机最多保留当前推荐之前的五轮。历史读取使用原来的来源选择和数量限制，并校验请求用户、来源与平台账号登录代次。条目的 `playlist.externalId` 配合来源 `instanceId/accountId` 可用于显式导入。

当日结果和历史保存在数据库中。内置网易云账号在服务重启并重新校验原登录后可继续读取；退出后重新登录会建立新的历史。数据库为每个 MusicFS 用户保留最近更新的 64 个范围，范围由所选实例、配置、账号绑定和数量限制确定。

来源层仍使用最多 64 项、8 MiB 的内存缓存和两个并发上游请求。一次上游调用最多 10 秒，连同等待的总预算为 20 秒。该短缓存最长有效 2 分钟；浏览器再次打开当日推荐时直接读取保存的整页。打开推荐或回看历史均不会导入歌单、同步曲库或写回远程平台。

## 当前边界

网易云公共歌单推荐返回 `personalized=false`、`nextCursor=null`。曲目能否播放取决于来源的播放能力。

公开接口见 [OpenAPI](../contracts/openapi.yaml)及 [HTTP Schema](../contracts/http.schema.json)。
