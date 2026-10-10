# NAS 与 Bilibili 插件增量实施计划

状态：2026-10-10，全部待实现；本轮只修改设计文档，不变更程序、配置 Schema 或 OpenAPI。依据：[NAS 设计](../nas-providers.md)、[Bilibili 设计](../bilibili-provider.md)、[开源参考](../provider-references.md)及[共同架构](../../../docs/architecture.md)。

## 1. 顺序与可独立交付范围

[增量协作图](../diagrams/nas-bilibili.puml)保留可编辑 PlantUML 源码；本次未渲染，不提供未经核对的图片。

先验证依赖与协议，再扩展受控资源读取，之后分别完成 SMB、NFS 和 B 站闭环。B 站按“匿名公开视频 + 公开收藏夹 → 音频区曲目/独立歌单 → 可选账号与私有收藏夹”推进。NFSv4、平台写回、推荐与搜索不阻塞前两类 B 站歌单接入；原生 NAS 不改变既有 Local Files 挂载模式。

| 任务 | 依赖 | 交付及完成门槛 |
| --- | --- | --- |
| NP-00 参考与 PoC | 无 | 固定依赖 SHA/许可；分别跑 SMB 只读、NFSv3 只读、B 站匿名音轨、收藏夹和 `au/am` 查询。记录工具链、平台限制和失败，不能用 mock 替代实机 |
| NP-01 冻结契约 | NP-00 | 版本化 Worker/Host 远端资源、错误、版本证据、取消、范围能力与配置字段；更新 OpenAPI/Schema 和兼容测试；设计来源建档与资产迁移 |
| NP-02 受控资源通道 | NP-01 | 从本地专用读取抽出可取消、限范围、有版本的读取边界；远端 Worker 音频正文走二进制通道；本地播放、摘要、NCM 与处理链回归通过 |
| NP-03 SMB Provider | NP-02 | 多实例配置与凭据、分页扫描、持久来源、随机读取和生命周期；通过真实 SMB + Server/Web 原流与处理播放、断线/变更测试 |
| NP-04 NFS Provider | NP-02 | NFSv3 export/RPC 配置、UID/GID、句柄失效处理；独立通过同等链路与 root/export 替换测试，不能用 SMB 结果替代 |
| BP-01 匿名视频音轨 | NP-02 | ID/链接规范化、分 P 资料、独立音轨、在线资产建档与受控 CDN 读取；无用户 Cookie 的真实播放、定位和到期恢复 |
| BP-02 公开收藏夹 | BP-01 | 收藏夹分页、分 P 展开预览与策略、RemotePlaylist 导入/绑定、保守同步；大于一页、失效项、重复项和私有化失败测试 |
| BP-03 音频区与独立歌单 | NP-02、BP-01 的公共在线读取边界 | `au` 曲目和 `am` 歌单独立适配、完整分页与播放；按各自证据发布能力，失败不影响收藏夹 |
| BP-04 可选账号 | BP-02 | 评估并接入实际授权交互、重启校验、退出/过期、本人私有收藏夹及隔离；登录不是匿名功能的启动条件 |
| NP-05 交付与联合验收 | 所发布插件对应任务 | 官方插件注册/发现、构建与制品说明、示例配置、Windows/Linux、真实 NAS 和浏览器证据；只打包已通过的能力 |

任何任务未满足能力门槛时保持未实现/实验性，错误不转换为空成功。每项证据记录提交、命令、样本许可、环境、实测结果及剩余边界；不预填通过状态。

## 2. 拟修改模块与仓库分工

| 仓库 / 模块 | 拟修改职责 |
| --- | --- |
| Server `src/plugins/smb`、`nfs`、`bilibili` | 各平台适配、只读连接、枚举/解析、能力声明；按职责拆分文件 |
| Server `src/pluginproto`、`pluginhost`、`sourceaccess` | 远端资源、任务授权、二进制读取、取消、版本证据；不把远端资源伪装为 `os.File` |
| Server `src/library`、`onlinehost`、`playback` | NAS 扫描发布、在线来源/资产建档、代理与处理输入、版本固定和恢复 |
| Server `src/playlists`、`storage` | 收藏夹/音频歌单命名空间、展开策略、provenance、保守同步及必要迁移 |
| Server `src/config`、`pluginconfig`、`bootstrap`、`plugincontrol` | 严格实例配置、秘密引用、目标限制、能力与动态生命周期；沿用现有管理实现 |
| Server `src/app/musicfs-plugins`、`scripts` | 接入现有统一插件程序的角色选择与发现；保留独立 Worker 进程，不把设计中的插件提前计入已构建数量 |
| Server `docs/contracts` | 精确 OpenAPI/事件/SDK 由 Server 维护；此次只写计划，实施时同步冻结 |
| Web | NAS 配置与状态、B 站链接导入、收藏夹/音频歌单类型、多 P 预览、可选账号与权限错误；复用已有播放会话 |
| Client / 主仓库 | 主仓库维护共同语义和图示；桌面 Client 评估新增来源类型、能力与错误兼容，不承诺桌面实现完成 |

## 3. 契约与兼容要求

- 现有 Local Files `Resource` 与在线“仅元数据”模型不能无版本地替换。新增远端描述采用可区分类型与能力协商，旧 Host 拒绝不支持的协议；旧客户端仍使用返回的 streamUrl，不感知 NAS 路径或 CDN 凭据。
- 用户公开 DTO 只包含允许展示的来源信息、能力、状态及受鉴权引用。NAS 连接秘密、内部二进制通道和 B 站授权请求头保留在 Host/Worker。
- 资产版本、attempt、Range、处理后时间轴和恢复预算继续遵守共同架构；受限或预览内容不得标为完整可播。
- 收藏夹和音频歌单复用 RemotePlaylist 接口族，类型与身份独立。集合查询、导入/绑定和可播放性分开；集合失败不清空已发布条目。
- 配置修订、运行代次、drain/force、后台租约一并适配；重新启用不能让旧响应覆盖新状态。

## 4. 验证安排

`tests/unit` 覆盖 ID、路径、映射、分页和版本判断；`tests/contract/plugins` 覆盖握手、资源/错误 Schema 与旧版本拒绝；`tests/integration/plugins` 启动真实 Worker 并注入断连/取消/迟到响应；Server E2E 验证扫描/同步发布、权限、代理与处理播放。真实 NAS、真实 B 站、真实浏览器分别留证。

共同验收编号为 NAS-01（SMB 闭环）、NAS-02（NFS 闭环）、NAS-03（离线/根替换/内容变化）、BILI-01（匿名视频）、BILI-02（公开收藏夹）、BILI-03（音频区曲目/歌单）、BILI-04（可选账号与私有收藏夹）、REMOTE-01（跨用户、取消、停用及凭据隔离）。完整场景细节分别见两份插件设计；尚无自动测试或线上验收结果。
