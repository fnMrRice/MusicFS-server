# P1：配置、服务、鉴权、存储和任务

状态：配置、鉴权、SQLite 与扫描任务基础已落地；具体通过项及未覆盖故障见[实施记录](../validation/progress.md)。返回[实施入口](../implementation.md)。桌面启动器和完整用户管理仍不在本期。

## 1. 拟建模块与测试

| 生产位置 | 职责 | 对应根 tests 位置 |
| --- | --- | --- |
| src/app/musicfs | 参数入口、调用 bootstrap、进程退出码 | tests/e2e/startup |
| src/bootstrap | 组装、就绪、资源清理和有界退出 | tests/unit/bootstrap、tests/integration/recovery |
| src/config | 加载、合并、校验、脱敏视图 | tests/unit/config |
| src/logging | slog 配置、请求关联、敏感字段过滤 | tests/unit/logging |
| src/api | 路由、中间件、统一响应、健康检查 | tests/contract/api、tests/integration/api |
| src/auth | 引导凭据、Cookie 会话、CSRF、用户上下文 | tests/unit/auth、tests/integration/api |
| src/storage/sqlite、src/storage/migrations | 连接、迁移、事务、版本和备份接口 | tests/integration/storage |
| src/jobs | 持久状态、调度、重试、取消和代次 | tests/unit/jobs、tests/integration/recovery |

## 2. 实现步骤

### S-P1-01：明确配置来源与运行目录

1. 当前配置为 configs/server.example.yaml，含监听地址、dataRoot、mediaRoot、cacheRoot、插件注册/实例及启用状态、允许的音乐根与资源上限；示例不带个人路径/凭据。解析、修订写入和运行入口已按[YAML配置](../configuration.md)落地，旧JSON配置不兼容；[配置验收](../validation/configuration.md)与后续动态管理分开记录。
2. 普通启动参数依次合并内置默认、YAML、显式环境变量和命令行；插件/实例启用开关以 YAML 为唯一持久来源，不允许环境变量或命令行静默覆盖管理结果。区分缺省与 false/空值，拒绝未知字段、重复键、多文档及无效类型；具体支持范围见 YAML 配置补充。
3. 本地默认 loopback；非 loopback 启动必须满足远程鉴权和安全传输配置，不因开发方便默认为公网匿名服务。
4. 将音乐根与可写数据/内容/缓存目录分离。元数据写回默认 false；只读扫描不要求音乐目录可写。
5. 验证目录权限、可创建性、磁盘位置与数据库单实例占用。避免两个服务实例同时把同一 dataRoot 当自己独占。
6. 敏感配置通过引用或受保护配置读取，普通日志/健康检查不得返回 token、密钥、全部音乐路径。

测试：覆盖相对路径解析、Windows 盘符/空格、中文路径、Linux 挂载路径、缺失目录、无权限、冲突覆盖、音乐根与缓存危险重叠。验收：错误配置在接收业务请求前失败，英文错误可定位字段，不泄密。

### S-P1-02：组装和生命周期

1. 入口解析 serve、配置路径及必要管理子命令，实际逻辑进入 bootstrap；main 不包含数据库或业务处理。
2. 依次完成配置校验、数据目录占用、DB 迁移、核心模块构建、插件登记、HTTP listener。任一步失败逆序释放已获得资源。
3. live 表示进程可响应；ready 仅在服务依赖就绪后为真，返回产品/协议/实例身份。
4. 收到停止信号时先转为 draining，拒绝新任务/新会话，再停止调度、取消可取消工作、等待文件发布安全边界、关闭进程/流/DB。
5. 记录启动实例 ID 和任务恢复范围。超时收尾须报告未完成任务，不能无界等待 worker 或丢弃写回恢复日志。
6. P1 按独立启动服务验收。受 Client 管理的租约模式在 P8 增补服务端能力；本阶段不伪造桌面生命周期已完成。

测试：每个启动阶段注入失败、端口占用、重复启动、正常退出、超时退出、持久任务中断后重启。验收：无残留锁、监听器和子进程；重启能判断哪些工作需要恢复。

### S-P1-03：HTTP 框架与错误

1. 使用标准库路由/HTTP server，统一 requestId、用户上下文、输入大小限制、JSON 解码和错误 envelope。
2. 普通控制请求有 deadline；流式音频单独定义空闲/取消策略，不能套用几秒写超时导致整曲中断。
3. 错误映射区分 INVALID_ARGUMENT、AUTH_REQUIRED、ACCESS_DENIED、NOT_FOUND、CONFLICT、RATE_LIMITED、UNSUPPORTED、TEMPORARY_FAILURE；保留 retryAfter。
4. requestId 从合法输入或服务端生成，日志使用结构化英文；日志级别不改变脱敏要求。
5. 对 body、header、URL query 和 FFmpeg 错误做定向脱敏，不把原始请求或配置全量转储。
6. 验证异常/panic 的处理边界；不能在已经开始的音频流后写入 JSON 错误正文。

测试：非法 JSON、未知字段、超大请求、错误 Content-Type、超时、断连、请求关联和日志脱敏。验收：控制面错误可被 Web 分类展示，敏感值不出现在日志快照中。

### S-P1-04：最小管理员会话

1. 预留稳定 userId，首版只启用本机管理员。引导凭据由显式配置或专用管理命令产生；只在用户要求显示的管理输出中出现，不在常规启动日志泄露。
2. POST /auth/session 校验 Authorization Bearer 中的引导凭据后创建可撤销随机会话；存储安全摘要、归属、有效期，不存可直接使用的明文会话值。
3. Cookie 为 HttpOnly、SameSite=Strict、适当 Path；远程 HTTPS 设置 Secure，HTTP 仅限明确 loopback 开发配置。
4. 首次 POST /auth/session 以显式 Bearer 和严格允许的 Origin 校验，不要求尚未取得的会话 CSRF。响应返回 csrfToken，GET /auth/session 可读取有效会话和 CSRF 状态。登录后的状态修改（含退出）校验会话 CSRF 和允许的 Origin，匿名请求不能创建任务或播放。
5. DELETE /auth/session 撤销会话，关联流/播放会话失去继续授权；新请求必须重新鉴权，活动流按明确的取消策略结束。
6. 登录失败限速；引导凭据可轮换，Cookie 不作为永远有效的 API 密钥。远程反向代理可信头必须显式配置。

测试：首次合法登录无 CSRF 可成功、错误/过期/撤销凭据、另一用户访问、跨来源登录/POST、登录后缺 CSRF、Cookie 属性、注销后流访问。验收：真实 audio 请求可使用同源 Cookie，Web 无需把 token 放 URL 或持久存储。

### S-P1-05：SQLite 和迁移

1. 使用 database/sql 和已固定版本的 modernc.org/sqlite；验证 Windows/Linux 运行，记录驱动版本、配置和依赖许可证。
2. DB 位于本机受支持数据目录，不把 SQLite WAL 数据库直接放进未经验证的网络挂载；音乐根仍可为 NAS。
3. 初始化 schemaVersion 和首批用户/会话、实例配置、任务、幂等记录表；library/playback/media 表随相应阶段迁移加入。
4. 使用版本化 SQL 和迁移记录；迁移事务、不可事务步骤与失败恢复分别定义。迁移前一致性备份，不能仅复制活动主数据库而遗漏 WAL。
5. 短事务和写入队列统一管理写操作，设置忙等待/取消策略。HTTP 网络请求和插件 RPC 不能持有长数据库写事务。
6. Repository 方法暴露业务需要的事务边界，错误可判断冲突/约束/临时不可用；未知属性用 NULL，不以 0 偷换未知。

测试：空库升级、重复启动不重复迁移、旧版本样本升级、坏迁移回滚、并发写、取消、磁盘满/只读、备份恢复。验收：schemaVersion 与实际结构一致，失败不会继续 ready。

### S-P1-06：任务状态与幂等

1. 定义 queued/running/succeeded/failed/cancelled 等持久状态，保存 jobId、owner、目标/预期版本、任务类型、代次、重试计数和时间。
2. Scheduler 取任务时获得唯一执行权，服务崩溃后旧执行权失效；重启不能同时启动原任务的两份有效执行者。
3. 任务用 context 传播取消和超时；插件 cancellation、数据库写入和文件发布各自说明能否立即取消。
4. 重试只针对标为可重试的错误，采用上限、退避及抖动；AUTH_REQUIRED 等需要用户处理的错误暂停，不忙循环。
5. 同一目标重复任务按业务合并或拒绝，Idempotency-Key 绑定用户、路由、请求摘要和有效期，换参数不能复用旧结果。
6. 发布结果校验最新代次和目标修订，旧任务即使晚完成也不能覆盖新状态。写回等外部副作用由阶段日志恢复，不可仅重跑函数。
7. 状态查询提供完成数/总数未知、lastSuccessAt 和错误；未知总数不要伪造百分比。P1 可轮询，事件通知在有消费者后增加。

测试：重复请求、参数冲突、取消竞争、重启恢复、旧代次晚完成、临时错误退避、凭据失效暂停。验收：接口返回任务 ID 与最终完成状态明确区分。

## 3. P1 完成门槛

- [ ] 空目录启动/健康检查/登录/注销/正常退出可通过真实 HTTP 验证。
- [ ] 未授权、跨用户、CSRF 请求不会创建任务或读流。
- [ ] DB 和任务在重启后保留，错误迁移和重复启动受控。
- [ ] 日志不含密钥，普通请求和音频流的超时策略不会混用。
- [ ] 服务仍不宣称能够扫描或播放；进入 P2 才实现来源和列表。
