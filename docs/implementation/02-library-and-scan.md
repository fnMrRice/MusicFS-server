# P2：Local Files、扫描与音乐库

状态：真实 Local Files worker、完整复核、稳定身份和固定修订列表已落地；实际 NAS、通知增量等边界见[实施记录](../validation/progress.md)。返回[实施入口](../implementation.md)。[插件任务](plugins.md)保留后续扩展要求。

## 1. 拟建模块与关键数据

| 位置 | 主要内容 | 测试位置 |
| --- | --- | --- |
| src/pluginproto、src/pluginhost | 握手、能力、实例、请求路由、取消和进程生命周期 | tests/contract/plugins、tests/integration/plugins |
| src/plugins/localfiles | 根目录枚举、过滤、稳定 externalId 映射、完整性报告 | tests/unit/plugins/localfiles、tests/integration/scan |
| src/sourceaccess | 授权根、资源引用解析、版本检查、受限打开和 input broker | tests/unit/sourceaccess、tests/integration/scan |
| src/library | 来源/资产建档、扫描暂存、库修订、查询与可用性 | tests/unit/library、tests/integration/scan |
| src/storage/sqlite | 实例、根身份、扫描代次、曲目/资产和快照持久化 | tests/integration/storage |
| src/api | 扫描任务与 source-tracks 列表 handler | tests/contract/api、tests/integration/api |

SourceTrack 使用稳定内部 ID，并保存 providerInstanceId + externalId 唯一约束。SourceAsset 是具体字节版本；记录可得的大小、codec/container、时长、码率和观测版本。RootIdentity 区分配置路径、预期卷/挂载身份和当前可达性。ScanGeneration 标记目标根、有效代次、完整性和上次成功结果。

## 2. 实现步骤

### S-P2-01：插件可用前的真实握手

1. 创建 Local Files worker 入口并由 Host 通过已配置可执行路径启动；不能让 API 直接指定任意命令或参数。
2. 验证 plugin/protocol 版本、角色、实例、能力与配置 schema；明确 library.scan、metadata.read、stream.resolve。
3. 配置按实例传入，允许根目录和排除规则与用户归属一同持久化。首版只加载明确列出的官方受信任 worker。
4. 控制通道保持 JSON-RPC 边界与 stdout 纯协议；日志走 stderr 并受宿主限长/脱敏。
5. 真实测试 worker 启动失败、版本不兼容、超时、取消、进程崩溃和重启；不能仅用同进程函数替身验收插件化。
6. 接入[插件管理](plugin-management.md)的插件/实例双层开关、准入与播放租约；禁用后不接受新扫描或新播放，已有任务按平滑停用规则收尾。来源禁用保留库、稳定身份与历史，不将其当成一次空扫描成功。

### S-P2-02：来源读取边界

1. Local Files 返回 opaque sourceRef、rootId、相对位置和 observedVersion；客户端 API 不得到 Server 原路径。RPC 不能传 os.File 或 io.Reader。
2. SourceAccess 对每次打开校验 user/instance/task/asset 和允许根，获取可关闭、可取消、绑定预期版本的读取句柄。
3. 评估并使用 Go os.Root 等防路径逃逸接口，测试符号链接/目录联接与替换竞争，不能只 EvalSymlinks 一次再普通 Open。
4. 仍校验普通文件、卷/挂载身份和策略：安全相对路径不代表挂载没有被替换，也不自动排除设备文件。
5. Gateway、ffprobe、摘要、音频和本地 Scraper 共用这一授权入口。进程外消费者通过独立二进制通道或任务限定的本机 input broker 获取数据，不获得任意目录访问 API。
6. broker 请求绑定任务、用户、输入资产、范围/用途、失效期，任务结束撤销；私密上游头留在 Server，外部 HTTP 请求不能自选 broker 目标。
7. 首版官方 worker 仍不是完整 OS 沙箱，明确其信任边界；manifest 的权限字段不能当成恶意进程已被隔离的证明。

测试：路径遍历、绝对路径、链接逃逸、打开前后替换、卷身份改变、另实例 sourceRef、取消后能力失效和原始路径不出 API。验收：所有音频读取入口受同一权限/版本规则控制。

### S-P2-03：完整扫描与增量提示

1. 根据配置生成扫描计划：根、递归深度/排除、支持扩展提示、全量/增量模式、扫描代次。扩展名只用于候选过滤，不能证明音频格式。
2. 先检查根与预期挂载身份，再枚举到 scan staging；逐文件报告发现、跳过、失败，限制队列和内存。
3. Provider 持久分配 externalId，Host 持久接口保存映射。初次建档不能用路径或 SHA-512 直接代替稳定曲目身份。
4. 文件重命名/移动结合可靠文件标识和复核证据更新映射；同内容多文件分别保留。歧义进入待核状态，不武断合并为移动。
5. 本轮完整且仍为有效代次时事务发布库修订；失败、取消或部分目录不可读保留旧可见数据，显示本轮失败状态。
6. 完整复核确认缺失时标不可用，保留歌单/来源历史；NAS 离线或失挂空目录不能视为扫描成功而批量删除。
7. 文件通知只作为增量任务提示，合并重复事件并定期全量复核；不要把 watch 丢事件当作来源永远不变。

测试：嵌套目录、中文/空格、无权限子目录、扫描中断、失挂空目录、重复文件、重命名、并发扫描和旧任务晚发布。验收：P2 不需要先算完整 SHA-512 才能显示或后续播放。

### S-P2-04：基础元数据和资产版本

1. 从文件名产生可显示 fallback，再用受控探测器读取标签、容器、codec、duration、码率、大小。探测失败只标 unknown/error，不丢失可识别文件条目。
2. ffprobe/解析适配有版本检查、参数白名单、超时、stderr 上限和输入权限；不得由文件名构造 shell 字符串命令。
3. CBR/VBR/unknown 分别表达，平台申报/实际探测/估算保留 origin；完整 SHA-512 和指纹由 P5 补全。
4. assetId 绑定具体版本。可靠探测到文件变更时新建资产；修改时间和大小只是线索，不能称为已验证内容摘要。
5. 活动读取保持同一打开资源，并监测可见变更；没有稳定版本保证时不承诺安全的跨请求字节续接。需要时使用已验证不可变缓存副本，而不是把路径重新打开后拼接。

测试：缺失标签、VBR、损坏音频、零字节、探测超时、探测期间改文件、同路径替换。验收：未知值在 DB/API 都为 null，旧播放资源身份不会随库刷新悄悄变化。

### S-P2-05：发布库修订并实现列表

1. 迁移加入查询所需的来源/资产/可用性索引；按用户和 Provider 实例过滤，权限过滤发生在分页之前。
2. 首请求选择已发布 libraryRevision，用稳定排序键加 sourceTrackId；生成绑定过滤/用户/修订的不可篡改或服务端登记游标。
3. 为本期实现物化的轻量列表快照/版本行，设置容量与保留期限；避免为每个 Web 游标长期占用 SQLite 读事务。
4. 扫描新发布不改旧游标结果。快照回收返回 CURSOR_EXPIRED，Web 重新读取首页；不能悄悄切到新库版本。
5. GET /source-tracks 返回共同契约字段和 nextCursor/libraryRevision；列表条目读取数据库，不能触发隐式扫描、哈希或整曲下载。
6. 实现 GET /provider-instances，返回当前用户可见实例的标识、名称、能力、可用性和账号状态，供 Web 选择扫描/列表目标；不返回凭据和 Server 磁盘根路径，不能让 Web 猜实例 ID。
7. 空库返回明确空列表；离线、不可播放、需登录与元数据未知状态分别表达，不统称“无歌曲”。

测试：多页并发扫描、同名排序、删除与新增、不同用户复用游标、游标过期/篡改、limit 边界。验收：真实 Web 能分页显示数据库中的扫描结果，无重复拼页或私人路径。

## 3. P2 完成门槛

- [ ] 从空库扫描真实受限目录，经 worker/Host 建档，而非直接硬编码列表。
- [ ] 重启不改变稳定 SourceTrack ID；移动歧义和重复文件不会丢出处。
- [ ] NAS 失挂、取消及旧代次不清空/覆盖已发布库。
- [ ] 列表分页固定版本并正确隔离权限。
- [ ] 受限读取、基础探测和进程失败测试完成；P3 可复用这些真实模块。

技术依据：[Go 受限文件访问](https://go.dev/blog/osroot)。具体 OS 和文件系统行为需在目标机器测试，不能将路径校验等同于完整内容快照保证。
