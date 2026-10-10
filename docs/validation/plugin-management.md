# 插件动态管理与配置接口验收

验证日期：2026-10-10，Windows 本机。本轮接口补齐、插件动态管理与 Web SSE 接入已完成。最终源码提交为 `aeab8a2`，796 个 Go 源码、测试、依赖、迁移与契约夹具文件已与 `runtime-validation` 验证副本逐一核对一致。HTTP 契约与运行说明见[插件管理](../implementation/plugin-management.md)。

## 1. 验证范围

| 验证 | 实际覆盖 |
| --- | --- |
| Manager、Gate 与真实 YAML/SQLite | 保存修订、幂等与所有者隔离、排空恢复原 Worker、force 取消但保留清理计数、force 后立即启用必须换代、旧 epoch 拒绝、设置重建与持久意图 |
| HTTP 与 SSE | 管理员／普通用户／匿名、Origin/CSRF、If-Match、幂等必填及冲突、操作隔离、未知字段、路径不回显；五类实际响应的 Schema 校验，local／audio／NCM 配置读取与 local／NCM 写后读取，首连与重连读取最新通知 |
| 本地实际 Worker | YAML 操作、drain 拒绝新播放、旧 WAV Range 与非零位置、旧会话释放后退出、重新启用、手动扫描、监控重建后新增文件入库 |
| 实际 FFmpeg | 旧处理会话在 drain 后仍输出真实6秒 PCM，并按时间定位输出2秒／660 Hz；新会话拒绝，关闭旧会话后活动计数归零，重新启用得到440 Hz输出 |
| 实际 NCM Worker | 解密字节与 Range、force 后旧会话流不可用且资源清理、重新启用后解密字节一致；输入原件前后逐字节不变 |
| 实际 FLAC 写回 | 本地／音频初始禁用后动态启用并完成真实写入；音频停用时415拒绝且能力同步不可用，重启插件后恢复；新增根不继承旧授权，原根路径和文件系统身份均复核 |
| 在线 Worker、推荐与远程同步 | 双实例真实进程，慢 RPC 排空与强制取消、重新握手、目标实例缓存失效而兄弟实例保留；真实 SQLite 同步任务的租约持续到成功发布或失败持久化 |
| 并发检查 | 管理器、播放、后台补全／识别、配置、本地库、插件入口与 HTTP 契约的定向 race；合并后另验证管理器、在线宿主和推荐，实际结果见下表 |
| Web | 提交 `12acdb9`：构建及43项单测通过；真实 Edge 开发模式验证单 SSE、停启刷新、断流重连重新读取、注销关闭 |

测试包括 `tests/integration/plugincontrol`、`tests/e2e/{library,audio,online,writeback}_management_test.go`、`tests/e2e/online_recovery_test.go`、`tests/integration/playback/plugin_lifecycle_test.go`、`tests/unit/library/lifecycle_test.go`、在线／推荐生命周期与远程同步生命周期测试。实际音频用例需要配置 FFmpeg/ffprobe；本轮使用本机 6.1.1 工具，未配置工具时的跳过不算音频验证通过。持久任务恢复用例检查后台领取任务在准入控制挂接后启动，并持有租约直到取消结果落库。

## 2. 已确认执行记录

| 检查 | 已有结果与依据 |
| --- | --- |
| 管理 HTTP 与 SSE 五类响应契约 | `go test ./tests/integration/plugincontrol -run 'TestPluginManagementHTTP\|TestPluginCapabilitySSE' -count=1` 通过，3.137 秒；使用 `testkit.ValidateHTTP` 校验 `PluginManagementSnapshot`、`ManagedPluginInstances`、`PluginOperation`、`ManagedPluginConfiguration`、`CapabilitiesChangedEvent` |
| 冻结基线完整检查 | 基线 `2ffd9c4` 的 `build/runtime-snapshot-final.log` 全部通过；E2E 127.695 秒、插件单测 58.663 秒，其余包沿用对应内容的已通过缓存 |
| 最终源码与静态检查 | `aeab8a2` 的 796 个验证输入文件与验证副本一致；最终 `go vet ./...` 通过 |
| 合并后最终补跑 | `build/runtime-integrated-confirmed.log`：完整 E2E 143.132 秒、HTTP 契约 57.589 秒、在线宿主单测 0.046 秒，全部通过 |
| 合并后定向 race | 管理器 47.789 秒、在线宿主 2.237 秒及修复后 2.550 秒、推荐 8.070 秒，均通过 |
| 冻结阶段定向 race | 管理器 48.013 秒、本地库 11.410 秒、在线宿主 5.792 秒、推荐 1.422 秒、写回宿主 5.840 秒，均通过；本地库、写回等未受后续修复影响的证据继续有效。更早八包定向结果保留于 `build/interface-race.log` |
| Web 自动检查 | Web `12acdb9`：生产构建、类型检查与 43 项单测通过；新增 6 项单测覆盖订阅、重复通知、重连、注销及非法事件 |
| Web 真实 SSE 联调 | 真实 Edge 开发模式、真实 Server 和转发代理；单连接、插件停启后刷新、事件连接中断后原 EventSource 重连并重读、注销关闭全部通过，1 项 E2E，测试主体 6.6 秒 |

## 3. 修正与验证结论

首次检查发现的管理配置模型重名和幂等断言问题已修正；`ManagedPluginConfiguration` 与表示导入声明的 `PluginConfiguration` 保持独立。合并账号约束后，`aeab8a2` 修复了已绑定账号的插件停用状态优先级，并使旧歌单测试通过正常登录流程取得平台会话。

合并后的全量检查 `build/runtime-integrated-final.log` 除两项修复前的旧歌单 E2E 外，其余包全部通过。修正后完整 E2E、HTTP 契约和在线宿主单测全部补跑通过。因此本轮结论为：**全量检查的失败已修复，最终所有 E2E 及相关包补跑通过**；这是全量检查结合修复后补跑的结果，不是单次全量运行全部通过。分阶段提交见[实施记录](progress.md)。

能力登记以配置身份和经过校验的握手为入口；不会自动发现、下载或安装任意程序。SSE 只负责通知能力需要重读，不广播配置或账号凭据。本次范围为接口补齐、插件管理配置与动态启停、Web 最小 SSE 接入。平台账号强制登录、打包发布、NAS 功能、插件管理界面及其他部署平台验收分别由相应任务记录。
