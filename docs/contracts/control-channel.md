# WebSocket 控制与状态通道

客户端通过 `GET /api/v1/control` 建立 WebSocket，子协议为 `musicfs.control.v1`。通道承载连接保活、播放控制、生命周期租约和状态通知。音频继续使用播放描述返回的 HTTP `streamUrl`；原流 Range、处理流逐步交付和 `timeRestart` 的语义不变。

消息定义见 [control.schema.json](control.schema.json)，业务正文复用 [HTTP Schema](http.schema.json)，HTTP 管理入口见 [OpenAPI](openapi.yaml)。

## 建连与鉴权

升级请求必须携带有效登录 Cookie、允许的 `Origin` 和指定子协议。凭据不放入 URL 或子协议字符串。客户端在升级后 5 秒内发送首条文本消息：

```json
{"type":"auth","csrfToken":"<当前会话的 CSRF token>"}
```

通过校验后，Server 返回就绪消息，客户端才能发送业务请求：

```json
{"type":"ready","instanceId":"<本次服务实例>","heartbeatIntervalMs":10000,"heartbeatTimeoutMs":30000}
```

Server 在每次心跳和命令时重新核验登录会话；登录过期或被撤销后，旧连接不能继续控制。重新连接必须重新完成首帧鉴权。账号或实例变化时，客户端废弃旧连接代次及待处理结果，不能把旧会话响应应用到新账号。

## 心跳与断线

客户端按就绪消息给出的间隔发送应用层 `{"type":"ping"}`，Server 返回 `{"type":"pong"}`。连接连续 30 秒没有收到客户端消息时关闭。心跳用于确认连接与鉴权会话可用，不自动延长播放会话或本地 Server 生命周期租约。

断线后采用有界退避重连。连接关闭不等于服务端撤销已受理命令，也不立即释放生命周期租约。通道不自动重放已发送的修改请求；当前 Web 在发送后断线或超时时返回本地 `OUTCOME_UNKNOWN`，停止自动控制并提示重新播放。尚未发送的 `NETWORK_ERROR` 可有界重试。业务层若显式重试结果未知的操作，必须沿用原幂等键，不能仅因重连生成新业务键。消息 `id` 只关联本次请求和响应，不替代业务幂等键。

切歌、停止或超时使播放创建结果不再需要时，Web 在有界窗口内保留同一连接的迟到成功清理：只停止该新建会话，不重新开始播放。连接已经断开且无法取得创建结果时，仍由服务端会话到期或登录撤销兜底回收。

## 请求与响应

客户端发送文本 JSON 请求，例如读取本机生命周期：

```json
{"type":"request","id":"status-01","method":"GET","path":"/lifecycle"}
```

`path` 从 `/playback/sessions` 或 `/lifecycle` 开始，不包含 `/api/v1` 前缀、主机、查询串、片段或百分号编码。`body` 满足对应业务请求的完整 Schema；`body` 和 `idempotencyKey` 仅在对应操作需要时提供。

| method / path | 语义 |
| --- | --- |
| `POST /playback/sessions` | 创建播放会话 |
| `DELETE /playback/sessions/{id}` | 停止本人会话并释放资源 |
| `POST /playback/sessions/{id}/seek` | 按曲目时间创建新尝试 |
| `POST /playback/sessions/{id}/recover` | 有限同源恢复 |
| `POST /playback/sessions/{id}/recover-source` | 按资产与时间证据恢复到指定来源 |
| `GET /lifecycle` | 读取本机实例归属及状态 |
| `POST /lifecycle/leases` | 创建本机客户端租约 |
| `POST /lifecycle/leases/{id}/renew` | 显式续租 |
| `POST /lifecycle/leases/{id}/release` | 显式释放租约 |

Server 返回 `{type:"response",id,status,body?}`，`id` 对应原请求，`status` 和 `body` 复用对应 HTTP 状态及 JSON 正文；无正文的成功响应可省略 `body`。错误正文沿用 `code/message/requestId/retryAfter`。播放请求仍检查当前用户、会话、资产与 `attemptId`；同一播放会话的 seek/recover 串行执行，迟到结果不得覆盖较新的尝试。

对应 HTTP 控制端点继续作为管理入口存在，使用原有 Cookie、Origin、CSRF 和幂等要求；客户端控制统一走 WebSocket，不把 HTTP 当作断线时自动切换的兼容通道。普通资料查询、管理接口与媒体读取继续使用 HTTP。

## 状态通知

事件使用统一信封：

```json
{"type":"event","event":"capabilities.changed","data":{"revision":"<当前修订>","sequence":1}}
```

`capabilities.changed` 在首连、重连、能力状态变化以及配置操作终态持久化后发送。客户端可重新读取当前用户可见来源、能力以及正在等待的配置操作。`revision` 是不透明值，`sequence` 仅标识本实例内的变化。

`activity.changed` 通知客户端按需重新读取已打开的视图。服务端按连接合并一秒内的变化，一项资源在同一条消息中只出现一次。例如：

```json
{"type":"event","event":"activity.changed","data":{"resources":["scans","tasks"]}}
```

| 资源 | 通知范围 |
| --- | --- |
| `scans` | 本人的来源扫描状态、进度及其元数据刷新结果 |
| `tasks` | 目录刷新队列，含扫描、逐曲元数据处理和取消收尾。管理员也可收到其他用户的队列变化提示 |
| `pluginJobs` | 本人的插件设置页后台任务进度 |
| `playlistSync` | 本人的远程歌单同步任务状态 |

通知只含资源种类，客户端使用现有授权 HTTP 接口读取详情。每次收到 `ready` 后应同步当前关注的视图，以补齐断线期间的变化。收到查询期间的新通知时，查询完成后再读取一次。服务端不保存待重放的事件。

实际 loopback 连接还接收 `lifecycle.changed`，`data` 为当前 `LifecycleStatus`，含实例、启动模式、状态、活动租约数与停机原因。首连发送当前快照，此后每秒采样变化；不列出其他会话的租约详情。停机通知尽力发送，不保证在连接关闭前送达；最终状态以连接状态和重新发现后的权威状态为准。

原 `/events` SSE 入口由本通道替代，不维护两套通知传输。状态通知用于促使客户端更新视图，权威业务状态和授权检查仍由对应服务负责。

## 本地租约

生命周期操作仍要求实际 loopback 连接；只有 `client-managed` 实例接受租约。租约有效 30 秒，建议每 10 秒通过 `request` 发送一次续租；连接 `ping` 不替代此请求。WS 断线后的宽限由原租约到期时间决定，重连不会额外延长期限。

租约继续绑定 `instanceId`、用户与登录会话；多个客户端可各持有效租约。续租使用上次成功序号加一，重复序号不延长到期时间，过期租约不可复活。正常释放、注销撤销及超时处理保持一致；最后租约结束后原子进入不可逆的 `draining`。详细收尾顺序见[本地生命周期](../operations/lifecycle.md)。

## 资源边界

每个登录会话最多 8 条连接。入站仅接受文本 JSON，单条最多 64 KiB；响应上限 1 MiB。请求 `id` 为 8—128 位 ASCII 字母、数字、下划线或连字符，`path` 最多 512 字符，`idempotencyKey` 最多 128 字符；业务端点可施加更严格的格式约束。

同一连接分别维护播放控制和生命周期命令两条队列，各最多等待 8 条，由独立工作协程处理。每条队列内部顺序执行；慢播放创建或恢复不会阻塞续租，客户端必须按响应 id 关联结果，不能假定跨队列响应按发送顺序返回。状态通知独立产生，响应与通知共用最多 32 条的出站队列；队列饱和时关闭连接，重连后重新读取权威状态。

首帧鉴权限 5 秒，写入限 5 秒。业务请求从服务端读取并受理消息时起共限 15 秒，包含排队和执行；排队期间已过期的命令不进入业务处理，不为出队后的执行重置期限。客户端应及时读取响应与通知，不能无限积压。业务请求超时或连接中断不保证操作未发生；未确认结果前不得盲用旧 attemptId 执行下一次修改。
