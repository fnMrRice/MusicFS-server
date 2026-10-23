# Server 插件管理

统一 Manager 管理本地/在线 Provider、Scraper、FFmpeg 和 NCM 的状态、工作租约与持久操作；数据库与 DSP 只读展示。配置见[YAML 配置](../operations/configuration.md)。管理 CLI、任意插件安装及自动升级仍未实现。

## 1. 生效边界

Server 提供插件/实例状态查询、开关与设置管理，要求管理员。停用只保存 `enabled: false`，当前 Server 进程继续使用运行开关；不会停止 Worker、关闭新工作准入、取消播放或任务，也不会改变运行代次。已有及新发起的工作继续按当前能力、权限和超时规则执行。正常停止并重启 Server 后，启动阶段读取保存开关，不再启动已停用对象。

停用不删除音乐库、歌单、账号引用、人工结果、媒体或匹配证据。账号撤销、卸载和数据删除仍是独立操作。需要立即终止工作时，使用对应任务/播放取消或停止 Server；插件停用接口没有强制停止语义。

## 2. 层级与状态

| 对象/字段 | 规则 |
| --- | --- |
| pluginId / instanceId | 插件总开关作用于全部实例；各实例仍有独立保存开关和稳定 ID |
| 插件项 version | 必填发行版本；最多 128 字符的有效 SemVer，与实例状态、配置修订及 Server 版本分离 |
| configuredEnabled | 对象自身已保存的开关，插件与实例分别报告 |
| effectiveEnabled | 按当前进程运行开关及必需配置计算，不按尚待重启的保存开关计算；不等于已就绪 |
| acceptingNewWork | 当前运行开关、依赖、权限与运行状态满足准入；配置重建的 draining 期间为 false |
| runtimeState | disabled、pending_configuration、starting、ready、draining、failed；停用保存本身不改变状态 |
| generation | Worker 生命周期及配置重建的代次；停用保存不推进代次 |
| reason | 保存开关与当前运行开关不一致时为 `RESTART_REQUIRED`；其他原因包括配置不足和依赖不可用 |
| restartRequired | 快照报告当前待重启开关或 diskChanged；操作收据记录该次保存后全局是否仍有待重启开关，历史标志不随撤销/重启改写 |
| revision / appliedRevision | 已保存修订 / 已完整应用修订；存在待重启开关时不能将 appliedRevision 推进到新修订 |
| operationId | 已保存操作的跟踪 ID；停用保存完成即可 succeeded，不表示 Worker 已停止 |

停用插件不改写实例自己的保存开关，停用一个实例不改写兄弟实例。重启后只有插件和实例都启用才可启动。父插件在当前进程已停用时，可保存子实例启用意图，但子实例不能越过父开关运行。运行故障不自动改写保存开关；重启后已停用对象不因自动扫描或恢复旧任务自行启动。

## 3. 操作与修订

1. 停用通过现有修订检查、幂等记录及持久化流程保存，不调用运行时排空或强制取消。HTTP 保留 202 操作收据；`status=succeeded` 表示保存完成，`restartRequired=true` 记录保存时全局仍有待重启开关，不能显示为已经停止；当前是否需要重启应重新读取管理快照。
2. 状态请求为兼容旧客户端仍接受 `mode=drain/force`，省略时保留默认 drain。该参数已废弃；两者在停用时行为相同，均不排空、取消或停止 Worker。新客户端应只提交 `enabled`，并读取重启提示。
3. 重启前重新启用可撤销待重启停用意图。若 Worker 和配置未变，不重建 Worker 或改变代次；若对象原已停用，则按启用流程启动。重新读取快照以确认是否仍有其他待重启变更。
4. 启用和实例设置继续动态应用；设置变更仍关闭目标实例的新工作准入，等待旧租约结束后重建。待重启停用不能被后续设置应用顺带应用，也不能因另一个实例完成应用而错误推进 `appliedRevision`。
5. Server 重启将旧未完成操作记为 superseded，再按当前保存配置初始化；当前状态以新快照为准。配置只读、修订冲突及保存失败仍明确报错，不以数据库开关悄悄替换损坏配置。

工作租约和运行代次继续用于配置重建、故障、资源回收及 Server 退出，防止晚到结果覆盖新配置；不再用于插件停用后的热排空或强制停止。

## 4. 管理入口与客户端兼容

以下路由均以 `/api/v1` 为前缀，管理端点要求管理员。

在普通插件后追加必需数据库、官方 DSP 增益、均衡器、压缩器、混响及内置算法目录中的声道转换条目。可选字段包括 `required`、`stateMutable`、`storage`、`dsp`、`builtin`；基础设施和固定内置算法只读且没有普通实例。数据库 `required=true`，状态修改返回 `UNSUPPORTED`（HTTP 415），不会保存停用或创建重建操作。`storage` 展示后端、模式和可用性；`dsp` 展示配置参与状态，并保留各节点在完整链中的原始位置、模式、版本和参数；声道转换通过 `builtin.capabilities` 声明支持布局，实际输出以播放描述为准。目录信息不表示正在处理，不泄露程序路径、参数或凭据。具体语义见 [DSP](dsp-plugins.md)、[声道与效果](dsp-effects.md)及[数据库](storage-plugins.md)。客户端需容忍 `instances=[]`，并以 `stateMutable=false` 或 `required=true` 阻止普通启停操作。

`GET /admin/plugins` 的每个插件项必须包含有效 `version`。七种普通随包插件从 [sdk/bundledplugin/releases.go](../../sdk/bundledplugin/releases.go) 读取与 Worker 同源的发行声明；没有实例或尚无有效握手版本时仍返回声明，包括未启动、待配置、已禁用和启动失败的情况。成功握手的有效版本优先，不使用 Server 版本、实例修订或空字符串填充。未知插件 ID 仍由配置及管理层拒绝。版本可展示不等于 Worker 已就绪，客户端仍须读取实例运行状态；旧 Server 缺字段时的界面兼容提示不放宽当前 API 必填约束。

本地音乐 `displayCount` 由宿主统计有效授权根，单位为“个允许访问的目录”，待配置、已禁用或 Worker 未运行时仍展示。添加、编辑和移除音乐目录不改变已确定的授权范围；Worker 不接收未添加路径。目录配置 GET 通过 `rootImportDefaultsSupported:true` 声明逐目录覆盖能力，`roots[].importDefaults` 返回有效值；PUT 可在 `musicRoots[]` 内保存对应目录的覆盖，不改变其他目录。客户端把插件管理中的权限数量与音乐源中的已添加列表分开呈现，完整兼容规则见[目录管理](plugin-configuration.md)。

两条插件列表默认只返回当前 MusicFS 用户的在线账号实例，共享非账号插件实例仍保留；管理员显式设置 `allUsers=true` 可查看所有用户实例，省略或 `false` 均保持本人范围。管理快照的账号实例携带 `ownerUserId`，可选 `accountName` 仅来自当前已验证、未过期且可用的平台账号显示快照，未登录或离线时省略；该字段不能作为授权依据，也不包含令牌、Cookie 或登录链接。

部署在在线插件实例 `instanceId` 同级配置 `ownerUserId`，缺省兼容为 `admin`；不同 MusicFS 用户、同一用户的不同平台账号使用独立 UUID 实例并分别登录。当前产品仍只有 `admin` 登录入口，尚无多用户创建/注册功能；配置归属不等于提供新用户登录。`allUsers` 仅扩大管理展示，不扩大 `/online-instances` 或账号登录/退出请求的用户范围。旧客户端须兼容可选字段；原来依赖全量管理列表的管理员客户端应显式传 `allUsers=true`。配置及凭据边界见[YAML 配置](../operations/configuration.md)。

| 路由 | 作用 |
| --- | --- |
| GET /admin/plugins | 保存/应用修订、restartRequired、插件身份及必填发行版本、实例开关及运行状态 |
| GET /admin/plugins/{pluginId}/instances | 实例保存开关、当前有效开关、状态、活动租约和重启原因 |
| PUT /admin/plugins/{pluginId}/state | 保存总开关；停用需重启，启用可动态应用 |
| PUT /admin/plugin-instances/{instanceId}/state | 保存实例开关，采用相同生效规则 |
| GET /admin/plugin-operations/{id} | applying/draining/succeeded/failed/superseded、restartRequired 及脱敏错误码 |
| GET/PUT /admin/plugin-instances/{id}/configuration | 配置声明与安全投影；按声明校验写入并排空重建 |
| GET /admin/plugin-instances/{id}/directories | 管理员浏览 Local Files 允许根及直接子目录，待配置时也可使用 |
| GET /control | 登录用户的 WebSocket 控制与失效通知，不包含管理员配置 |

修改要求 `If-Match` 配置修订及 `Idempotency-Key`。同键同参数返回原操作，换参数复用键报冲突；修订不符返回 412。所有修改继续校验 Cookie、允许的 Origin 与 CSRF，不把令牌放入 URL。管理 CLI `musicfs plugins list/status/enable/disable` 仍为后续计划。

普通来源和播放按当前运行状态准入。停用已保存、尚未重启时不返回 `PLUGIN_DISABLED`；重启后实际停用，或设置变更正在排空时，才按原准入规则拒绝新工作。缺必需配置的实例保持 `pending_configuration`，有效开关和准入为 false；没有待重启开关时原因为 `CONFIGURATION_REQUIRED`。来源列表保留条目并明确可用性。

配置投影不返回音乐根路径、工具路径、程序路径或凭据；路径为 write-only。省略已有根路径保留原绑定，已有根 ID 不能改绑目录；新增目录使用新 ID。`musicRoots/watch/importDefaults` 整组替换，未提供的顶层字段保持不变；`musicRoots[]` 中已有根省略 `importDefaults` 时保留该根原有覆盖，避免旧客户端写入抹除逐目录设置。改配置的排空与停用保存是不同操作。

保存修订及运行状态变化可触发 WebSocket `capabilities.changed`，数据仍仅为 `{revision,sequence}`。每次首连/重连发送当前通知，客户端重新读取授权列表；通知不意味着能力已经停用。`/control` 使用 `musicfs.control.v1`，Cookie/Origin 升级检查及首帧 CSRF 鉴权；每个登录会话最多 8 条连接，建议 10 秒心跳，30 秒无客户端消息关闭，每次心跳和命令复核登录。原 `/events` SSE 已由该通道替代，完整格式与重连语义见[控制通道](../contracts/control-channel.md)。

客户端须区分保存开关、运行状态和重启提示；仅凭 `configuredEnabled=false` 或操作 `succeeded` 显示“已停用”会误报。实例列表用 `reason`、全局页用快照 `restartRequired` 提示，操作标志只代表保存当时的状态。
