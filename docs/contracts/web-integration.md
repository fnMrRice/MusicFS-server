# Web 接口接入边界

本页索引服务端接口及前端必须保留的语义。接口存在不代表对应页面已接入；页面状态由 [Web 开发方案](../../../web/docs/react-development.md)维护，字段以 [OpenAPI](openapi.yaml)和 [HTTP Schema](http.schema.json)为准。

| 场景 | 接口与说明 |
| --- | --- |
| 本地曲目、专辑、歌手搜索及组合筛选 | `/library/search`、`/library/facets`、`/source-artists/{id}`；见[本地查询](../library/catalog-query.md) |
| 固定全部结果、恢复与取消 | `/library/queries`、`/library/queries/{id}`；使用同一固定结果分页 |
| 在线四类搜索、详情及关联作品 | `/online-instances/{id}/search` 及专辑/歌手子路径；见[在线来源](../providers/online.md) |
| 本人歌单搜索和来源歌单列表 | `/playlists?q=...`、`/online-instances/{id}/playlists`；见[个人歌单](../library/playlists.md)、[来源歌单](../library/source-playlists.md) |
| 批量展示来源引用 | `/source-refs/lookup`、`/online-instances/{id}/tracks/lookup`；保留顺序、重复项和单项错误 |
| 个人资料、收藏、最近播放 | `/me/profile`、`/me/favorites`、`/me/recently-played`；见[个人数据](../library/personal-library.md) |
| 插件管理和配置 | `/admin/plugins` 及操作查询；见[插件管理](../plugins/plugin-management.md) |
| 管理员日志等级 | `GET/PUT /admin/logging`；全局与插件覆盖整组替换，当前进程生效，重启恢复配置文件；见[日志](../operations/logging.md) |
| 播放控制与连接保活 | `/control` 的 `musicfs.control.v1` WebSocket；发送创建、停止、seek、recover、recover-source 请求及心跳；[控制协议](control-channel.md) |
| 能力与本机状态通知 | WS `capabilities.changed` 触发重新读取本人可见来源与能力；loopback 连接另接收 `lifecycle.changed` |

## 接入约束

- `availableOnly` 只表示扫描观测可用，不能命名为“仅可播放”；`playableOnly` 尚不支持。实际播放重新检查授权、版本与格式。
- 本地“播放全部”逐页读取固定查询直到结束；游标过期须刷新，不能把新首屏拼接旧结果。在线查询尚不提供固定全部结果。
- 同名曲目、专辑和歌手不自动跨来源合并；本地专辑为显式集合，歌手为来源内署名。在线各来源保留独立游标、错误和顺序。
- 三平台业务要求已校验的平台账号；账号失效不能读取旧缓存，也不能退出 MusicFS 会话。资料可见不代表完整音频可播。
- 最近播放仅在真实 `playing` 事件后报告当前会话/attempt；资料和收藏使用服务端账号数据。头像上传、跨设备搜索记录尚未实现。
- WS 升级检查 Cookie/Origin，5 秒内首帧发送 CSRF；ready 后按返回间隔发送 ping，30 秒没有客户端消息时关闭。首连和重连均重新读取授权来源，注销时关闭连接；事件不代替查询。插件操作成功需结合运行状态与 `restartRequired` 展示，停用保存后重启生效。
- 音频继续通过 HTTP `streamUrl` 交付，WS 断线不代表音频缓冲已停止，也不撤销已经受理的控制请求。已发送修改在断线或超时后返回本地 `OUTCOME_UNKNOWN`，不自动重放，当前 Web 提示重新播放；未发送的 `NETWORK_ERROR` 可有界重试。业务层显式重试未知结果必须沿用原 `idempotencyKey`；同会话 seek/recover 保持串行及 attempt 检查。
- 页面验证应覆盖空结果、无权限、离线、限流、部分失败、迟到响应、过期游标及未知总数；合成测试不能替代真实端到端验收。
