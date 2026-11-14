# 个人资料、收藏与最近播放

这组接口由 Server 保存账号数据，以登录会话的真实 `user.id` 隔离。当前登录方式仍是配置的管理员凭据；接口没有引入注册、多账号管理或平台账号同步。路径均以 `/api/v1` 开头，协议定义见 [OpenAPI](../contracts/openapi.yaml) 与 [HTTP Schema](../contracts/http.schema.json)。

收藏和最近播放使用普通歌单的条目表。通过 `GET /playlists/favorites`、`GET /playlists/recently-played` 取得账号内的固定身份、类型和当前修订，再用 `GET /playlists/{id}/snapshots/{revision}` 读取曲目及强制来源。普通歌单的类型为 `user`。特殊歌单只提供当前修订，过期返回 410。

收藏的身份是曲目。无来源曲目也可收藏。同曲目的多个来源共用一条收藏；不带请求体的 PUT 保留现有选择，也可显式提交 `{"forcedSourceTrackId":"..."}` 固定版本，提交 null 恢复默认。最近播放保存实际来源；开启独立历史的内容类型交给对应插件写入历史。

## 资料

| 请求 | 行为 |
| --- | --- |
| `GET /me/profile` | 返回 `user`（真实 ID、角色）、`nickname`、`avatar` 和 `avatarUploadSupported` |
| `PUT /me/profile` | 请求必须提供 `nickname`；字符串先去首尾空白，最多 80 个 Unicode 码点，不接受控制字符；`null` 或空白字符串清空昵称 |

昵称由用户填写，未填写时为 `null`。当前 `avatar` 始终为 `null`，`avatarUploadSupported` 为 `false`；客户端使用默认图形。不能把虚构姓名、随机头像或来源平台头像当作本账号资料。资料更新成功返回当前资料；重复提交相同昵称不产生其他操作。

## 收藏

| 请求 | 行为 |
| --- | --- |
| `GET /me/favorites?limit=50&cursor=...` | 按收藏时间倒序分页，时间相同按曲目 ID 排序 |
| `PUT /me/favorites/{trackId}` | 保存本人的曲目，可选固定来源，返回收藏条目 |
| `DELETE /me/favorites/{trackId}` | 取消本人收藏，204；重复删除仍成功 |
| `DELETE /me/favorites` | 清空本人收藏，204 |

重复 PUT 保留原收藏时间。每个账号最多收藏 20,000 首，达到限制返回 `RATE_LIMITED`。离线的来源保留收藏及资料，播放时检查来源是否可用。

## 最近播放

| 请求 | 行为 |
| --- | --- |
| `GET /me/recently-played?limit=50&cursor=...` | 返回账号最近播放的来源曲目，按最近一次确认时间倒序分页 |
| `POST /me/recently-played` | 报告实际开始播放；必填 `sourceTrackId`、`playbackSessionId`、`attemptId`，成功返回条目 |
| `DELETE /me/recently-played/{sourceTrackId}` | 删除此曲目的账号播放记录，204 |
| `DELETE /me/recently-played` | 清空本人记录及对应去重凭据，204 |

客户端仅在播放器实际发出 `playing` 后报告；点击播放按钮、创建播放会话、预加载流、请求 Range 或收到播放描述都不构成最近播放。Server 校验播放会话属于当前账号及当前登录会话、尚未停止／过期、attempt 是当前 attempt，且播放资源与来源曲目一致。Server 无法证明用户听到了声音，此事件是经播放会话校验的客户端确认。

每个播放会话最多记录一次；同会话的重试、暂停后继续、seek 或 recover 后的新 attempt 不会重复刷新最近播放时间。会话已经记入某来源后，跨来源恢复也不增加第二条历史，改用另一 `sourceTrackId` 报告返回 `CONFLICT`。新建会话并实际开始播放才更新同一 `sourceTrackId` 的时间；列表中每个来源曲目最多出现一次。请求不接受客户端时间戳，使用 Server 时间。重复报告已停止／过期或被替换的 attempt 仍返回相应失效／冲突错误，不能借幂等绕过校验。

保留最近 90 天、最多 1,000 首来源曲目；读取或报告时清理超期及超量条目，离线期间不运行独立的定时删除任务。去重凭据最多留到对应播放会话到期。显式删除同时删除对应去重凭据；删除后，如客户端为仍有效会话再次报告新的 `playing`，记录可重新出现。删除不会停止播放，也不会删除音乐文件。

## 分页、权限和错误

两个列表返回 `{items, nextCursor, revision}`，每个条目包含 `musicTrack`、`track`、`forcedSourceTrackId` 与 `recordedAt`；`track` 复用来源曲目 Schema，包含当前资料和可用性，无来源时返回 null。`recordedAt` 在收藏中表示首次收藏时间，在最近播放中表示本次 Server 确认时间。

`limit` 为 1～100，默认 50；`nextCursor=null` 表示结束。游标绑定账号、集合种类、集合修订和音乐库修订，有效期 10 分钟。集合增删、最近播放顺序变化或音乐库修订改变后，旧游标返回 `CURSOR_EXPIRED`（410）；客户端应清空原分页结果并刷新，不能把新旧修订追加到同一列表。可在同一修订中重复取得同一后续页；不声明未知或未计算的总数。游标每账号最多同时保留 2,048 个，超额返回 `RATE_LIMITED`。清空或删除会同时撤销该集合已有游标。

全部接口要求登录；写请求另要求允许的 Origin 和 CSRF Token。所有查询、写入、删除都限定登录用户，跨账号来源／播放会话返回 `NOT_FOUND`。PUT、DELETE 为天然幂等操作，最近播放 POST 以有效播放会话去重，均不另要求 `Idempotency-Key`。不支持的查询参数、重复查询参数、空游标、非法分页和多余 JSON 字段返回 `INVALID_ARGUMENT`。
