# S-P2：文件通知、去抖与周期复核验证

日期：2026-10-10。配置与操作语义见[扫描说明](../scanning.md#文件通知与周期补漏)。此步骤使用已有`x/sys`依赖，不引入额外通知库；Local Files仍为0.3.0、`local-import-v3`。通知只调度正式扫描，不直接更新曲库，不增加动态插件启停或Web页面。

## 实现与验证范围

- Local实例 `config.watch` 默认关闭，严格YAML/JSON Schema、部分默认、时间关系、未知/null/越界拒绝、最多8个启用实例、配置快照独立。
- 已授权根句柄枚举，复用递归深度和排除规则；不越过符号链接授权，每实例最多256目录，每根最多50,000条目、每批128项。失败关闭已打开句柄并退为周期复核。
- Windows以已打开目录句柄作为NtCreateFile相对根，异步目录通知与IOCP；Linux用已打开句柄的`/proc/self/fd`绑定inotify。均为非递归监听，注册完成后原句柄可关闭，不重开用户提供的字符串路径。事件不保存文件名，只发送有界合并提示。
- 原生测试覆盖句柄先改名再注册、原路径替身不能触发、真实增删改事件、非递归、256/257界限、取消与并发幂等Close、目录删除后只报告一次失败。停止先取消，等待内核完成后释放缓冲/句柄。
- 调度测试覆盖启动full、运行期间百次事件只保留一个增量提示、持续变化不能无限推迟、目录超限仍周期full、paused任务停止监听/自动提交，退出完成。测试内部使用短周期验证，正式YAML下限仍30秒。
- 正式YAML Server+Local Worker E2E覆盖默认关闭、启用后自动首次扫描、新建目录及其后续子文件、深度/排除保护、改名保持来源ID、删除变missing、重启补扫离线新增、所有剩余原件逐字节不变；全部使用项目合成WAV和临时目录。

## 实际结果

Go 1.26.9；当前工作区固定依赖`x/sys 0.49.0`、`yaml/v3 3.0.5`等，没有修改依赖文件。Windows race使用MSYS2 UCRT64；Linux原生通知在Arch/WSL的`/tmp`文件系统运行，不把Windows挂载盘通知当作inotify验收。

| 检查 | 结果 |
| --- | --- |
| YAML默认/边界/Schema/官方示例 | 普通1.637s通过；配置定向race8.277s通过 |
| 安全目录scope/预算/取消 | 普通0.124s通过；定向race6.480s通过 |
| Windows原生通知5项 | race3.909s通过，vet通过 |
| Arch原生inotify5项 | race2.633s通过 |
| Windows调度3项 | 最终race5.340s通过 |
| Windows真实Server/Worker全流程 | 最终race14.002s通过 |
| 通知/配置/宿主及相关测试包vet | 通过 |

首次Windows实现使用ReOpenFile时目录重开被拒绝，真实通知测试未通过；随后换为锚定句柄NtCreateFile并修正IOCP取消完成等待。联合E2E还发现“发布成功后再拆旧监听”的重建窗口会丢失客户端随即写入的新子目录事件，现改成扫描前注册，不在发布成功后拆掉已建立的监听。暂停调度测试曾把新任务错误地立即标成功，已改为按任务独立状态后验证。这些前次失败不计为验收通过。

本轮不宣称NAS、macOS、ARM或其他文件系统通知可靠。超过限制、通知失败/溢出和注册竞争仍由周期完整复核兜底；权限/认证/离线进入paused后则暂停到服务重启，不会继续周期自动新建任务。Linux的完整宿主组合与最终全量检查由[实施记录](progress.md)补充，以上原生Linux证据不冒充真实Linux服务端到端。

实现依据：[NtCreateFile](https://learn.microsoft.com/en-us/windows/win32/api/winternl/nf-winternl-ntcreatefile)、[IOCP](https://learn.microsoft.com/en-us/windows/win32/fileio/i-o-completion-ports)、[CancelIoEx](https://learn.microsoft.com/en-us/windows/win32/api/ioapiset/nf-ioapiset-cancelioex)、[目录通知](https://learn.microsoft.com/en-us/windows/win32/api/winbase/nf-winbase-readdirectorychangesw)、[inotify](https://man7.org/linux/man-pages/man7/inotify.7.html)。
