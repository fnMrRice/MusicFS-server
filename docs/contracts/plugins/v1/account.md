# 在线平台账号控制面扩展 v1

机器定义：[账号 Worker Schema](account.schema.json)；与在线资料 Schema 并列校验本扩展的方法。

新增 `provider.online.account` 方法，沿用 `OnlineHello` 已绑定的 userId / instanceId、逐请求 taskId / timeoutMs 和精确上下文回显。资料协议修订为 online-authenticated-v2，所有业务调用使用本 Worker 当前已校验账号；调用方不能通过 accountRef 指定或冒用其他账号。accountRef 保留为空是控制通道身份约束，不表示匿名执行。旧匿名 Worker 的握手不再兼容。

请求为 `{context, request}`；`request` 包含 action、可选 loginId、userToken、developerToken、secret。动作包括 state、begin、poll、complete、cancel、check、logout，以及仅 Host 可用的 restore、signalCancel。后两者不可经 HTTP 调用。signalCancel 在网络轮询仍在执行时标记挑战取消；loginId 为 * 时立即阻止并取消在途业务请求，且不占普通业务并发槽，正式 cancel 随后收尾；不向 HTTP 返回其临时结果。

响应为 `{context, value: {view, secret?}}`。view 遵循 [HTTP 账号 Schema](../../platform-account.schema.json) 的 View；secret 是只允许受控 stdio 通道传输的凭据对象，Host 去除后才能返回 HTTP。Host 加密持久化，并在重启时 restore；恢复为 unverified，显式 check 后才可标为 signedIn。请求或响应不能记录到普通日志。

每个已绑定用户的 Provider 实例在本增量中只有一个平台账号；切换账号先退出该实例账号，多个账号使用多个实例。三个平台独立隔离，不能将账号共享给 Scraper。返回 accountId 是绑定身份；Apple API 不提供 Apple ID，此处为本地随机 ID。

登录网络请求只允许固定 HTTPS 平台域名、公开解析地址、固定路径和有界响应，不跟随任意重定向；QQ授权回跳只解析并检查约定域名、路径和 state。平台授权失败不使 MusicFS 会话失效。

当前没有账号事件推送，界面按受鉴权 GET / POST 查询状态。完整平台账号实测尚待用户授权；能力描述中的 account.login 不标为已验证支持，HTTP options 的 validation 固定为 liveAccountPending。

View.revision 是每次有效登录的随机代次，恢复后首次校验也产生新值；同一登录的再次 check 保持不变。分页游标及推荐缓存绑定该代次。业务入口未登录/未校验返回 PLATFORM_AUTH_REQUIRED，HTTP 映射 403；账号状态、登录、校验和退出入口不受业务登录门槛影响。
