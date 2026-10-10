# 本地 Server 生命周期租约

S-P8-06 提供 Server 侧协议和退出流程。桌面窗口、进程发现、安装器、升级器以及 Client 租约发送尚由客户端实现；本步用协议客户端验收。

## 启动归属

```powershell
musicfs serve --config server.yaml
musicfs serve --config server.yaml --startup-mode client-managed
```

默认 `standalone` 独立运行，不因客户端退出而结束，也不接受租约接管。`client-managed` 必须监听明确的 loopback IP，例如 `127.0.0.1:8080`；拒绝通配地址、非 loopback IP 和主机名。模式只由本次启动参数确定，配置文件及 HTTP 均不能动态切换。

每次启动生成新的 `instanceId`。持有 `dataRoot/server.lock` 独占锁后，将实例、模式、实际 PID、UTC 启动时间写入该文件并同步；其中没有凭据。旧记录不改变新启动的归属，也不能作为当前进程存活的证明。在线状态从鉴权接口读取，重启后旧租约全部失效。

服务就绪后有 30 秒建立首个租约；未成功认领则进入停机。每个租约有效 30 秒，且不超过鉴权会话剩余寿命。建议每 10 秒续租。生产 CLI 不提供缩短或无限延长租约的参数；可信 Go 嵌入入口 `ServeWithOptions` 可为测试注入时钟和时间设置。

## HTTP 协议

以下路径均在 `/api/v1` 下。全部要求已登录 Cookie 和实际 loopback TCP 连接；POST 另需允许的 `Origin` 与 `X-CSRF-Token`。不信任代理头来判定本机来源。模型见 [HTTP Schema](contracts/http.schema.json) 的 `Lifecycle*` 定义，路由见 [OpenAPI](contracts/openapi.yaml)。

| 请求 | 正文/响应 |
| --- | --- |
| `GET /lifecycle` | 状态含 `instanceId`、`startupMode`、`state`、`activeLeases`、`leaseTtlMs`、首租约期限和停机原因；不列出其他会话租约 |
| `POST /lifecycle/leases` | `{instanceId,clientId}`，要求 `Idempotency-Key`；201 返回租约 |
| `POST /lifecycle/leases/{id}/renew` | `{instanceId,sequence}`；200 返回租约 |
| `POST /lifecycle/leases/{id}/release` | `{instanceId}`；200 返回结束状态，自然幂等 |

`clientId` 和幂等键是客户端生成的 8—128 位 ASCII 字母、数字、下划线或连字符标识，不含用户名、路径或机器秘密。租约绑定服务实例、用户和具体鉴权会话；相同用户的另一个会话也不能操作它。

新租约 `sequence=0`。首次续租发送 1，此后只发送上次成功序号加 1；相同序号重试返回相同到期时间，不额外延长。跳号或迟到的旧序号返回 `CONFLICT`。过期租约不能复活。同一会话、同一客户端的活动租约会合并；创建重试返回该租约当前状态，结束状态不重新激活。

模式为 `standalone` 返回 `UNSUPPORTED`；实例不符返回 `CONFLICT`，需要重新发现。没有权限的租约返回 `NOT_FOUND`。每实例最多 256 个活动租约，并限制为 4096 条创建幂等记录、租约记录及撤销围栏，达到边界返回 `RATE_LIMITED`。

## 停机与恢复边界

多个有效租约并存时，单个 Client 退出或崩溃不会关闭服务。正常退出应释放租约；会话注销会撤销该会话全部租约。崩溃依靠租约超时处理。最后租约结束后，实例在同一状态锁内不可逆地进入 `draining`，拒绝新租约和续租；新 Client 需要重新发现实例。

停机先关闭新操作准入并取消后台上下文，再关闭 HTTP、播放、任务、插件和存储；持久任务及媒体/写回日志由各模块下次启动恢复。生产正常收尾窗口为 10 秒。超过窗口返回失败，不宣称写入已经完成：CLI 以非零状态退出，由操作系统回收进程；嵌入调用者若继续运行，数据目录锁仍由清理线程持有，直到真实清理完成才释放，第二实例不能与旧后台重叠。

健康检查会报告未就绪；HTTP 监听关闭后无法继续查询停机状态。只保留可恢复日志并不等于任意文件系统故障均可无损完成写回，写回自身的阶段检查和人工恢复限制仍适用。
