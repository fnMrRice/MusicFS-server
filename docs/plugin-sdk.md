# Go 插件 SDK 与在线插件模块

状态：2026-10-20，工作区模块拆分。SDK 和在线插件仍由当前私有 Server 仓库管理，具有独立 `go.mod`、构建入口和测试；尚未创建独立远程仓库或发布版本标签。本次不新增平台业务能力，不代表真实平台账号或在线音频已扩大验收。

## 模块与依赖

插件实现按用途位于 `plugins/source`、`plugins/database`、`plugins/decoder`、`plugins/scraper`、`plugins/dsp`。完整目录与模块边界见 [插件目录](../plugins/README.md)。在线模块为 `plugins/source/online`，本地音乐源为 `plugins/source/localfiles`；数据库插件为 `plugins/database/sqlite`。

| 模块 | 负责内容 | 允许依赖 |
| --- | --- | --- |
| 根 Server 模块 | HTTP、账号归属、任务、音乐库、存储、插件进程管理 | SDK；在线实现仅用于测试 |
| `sdk` | JSON-RPC 帧与取消、协议 DTO、通用错误、实例 ID、在线结果校验、Provider Worker | 标准库及 SDK 明确列出的基础库 |
| `plugins/source/online` | 网易云、QQ 音乐、Apple Music 适配、三个独立程序与统一在线入口 | SDK 和插件自身代码 |

宿主生产依赖不能包含在线平台适配器；SDK 和插件模块不能反向导入 `src/`。依赖边界由 `TestPluginModulesHaveNoHostImplementationDependencies` 使用真实 Go 依赖图验证。公共 DTO 与上游请求实现分开，宿主不再为了读取 `Track`、`AccountView` 等类型导入平台 HTTP 客户端。

SDK 包入口：

- `sdk/pluginproto`：现有 Provider 协议、Manifest、双向 RPC、帧处理、取消及配置声明校验。
- `sdk/online`：在线平台公共 DTO 与现有官方来源的结果校验策略，不访问平台网络。
- `sdk/provider`：通过接口和工厂注入业务实现的在线 Worker；处理握手、作用域、截止时间、取消、消息大小和并发限制。
- `sdk/fault`：公共错误和随机请求标识。Server `domain.Error` 为同一类型的别名，保持 `errors.As` 行为。
- `sdk/instanceid`：规范实例 UUID 和已有实例路径兼容规则。

在线平台共用一个插件模块维护公共代码，分别生成 `musicfs-plugin-netease`、`musicfs-plugin-qqmusic`、`musicfs-plugin-applemusic`，各自只链接所属平台驱动，可以单独构建与部署。另提供 `unified` 装配包和 `musicfs-plugin-online --plugin netease|qqmusic|applemusic` 统一程序，方便只部署一个在线文件；仍按每个平台实例启动独立进程，平台选择必须与握手身份一致。SDK 不拥有平台登录、加密、HTTP 请求、Cookie 或平台显示名称。已知平台 ID、结果 URL 校验及最大允许能力目前仍是 SDK/宿主的官方来源策略，本次没有开放任意第三方身份、未知角色或音频能力。

## 构建与测试

统一构建仍使用根目录 `scripts/build.ps1` 或 `scripts/build.sh`。脚本分别进入模块，使用 `GOWORK=off` 构建；不依赖开发者本机的 `go.work`。

单独构建在线插件，在 `plugins/source/online` 执行：

```sh
GOWORK=off go build -mod=readonly ./cmd/musicfs-plugin-netease
GOWORK=off go build -mod=readonly ./cmd/musicfs-plugin-qqmusic
GOWORK=off go build -mod=readonly ./cmd/musicfs-plugin-applemusic
GOWORK=off go build -mod=readonly ./cmd/musicfs-plugin-online
```

Windows PowerShell 先执行 `$env:GOWORK = 'off'`，再执行对应 `go build` 命令。SDK 与插件模块分别执行 `go test -mod=readonly ./...`；根模块检查脚本同时包含两个模块，并继续执行宿主契约和真实 Worker 测试。

当前 `replace` 只连接仓库内的 SDK 与插件模块，便于一个工作区验收。对外发布前需创建并验证模块标签、固定对应版本、移除消费方的本地替换。SDK 和在线插件的版本可以独立演进；当前 `v0.1.0` 依赖声明不表示远程已存在该标签。所有仓库与制品继续保持私有。

## 版本与能力兼容

插件发布版本使用有界语义版本字符串，不再要求精确等于 `0.1.0`。通信兼容仍由 `protocolVersion=1.0` 和 `schemaRevision=online-authenticated-v3` 严格判断；版本号变化不会自动获得新方法或权限。

在线声明可以提供已知且已验证能力的子集。宿主分别验证 Manifest 与描述，并核对启用能力集合和显示身份一致；重复能力、未知能力、超出当前平台策略的能力、错误协议或声明不一致均拒绝启动。运行期间依据已验证声明展示与执行能力，不能用宿主默认能力覆盖插件自己的限制。新增上游能力仍须更新并验证宿主策略及协议，而不是通过提升插件版本绕过检查。

公共 HTTP 字段与账号/实例身份不变。Web 与 Client 继续读取 Server 返回的能力；本次无需修改浏览器交互。旧宿主可能拒绝新的插件发布版本或能力子集，因此首次迁移应先升级宿主，再替换插件。

## 分发与旧配置

默认安装包包含八个程序：Server、四角色本地插件集合、SQLite 存储、DSP gain、三个在线独立程序与一个在线统一程序。`musicfs-plugins` 仅包含 Local Files、FFmpeg、NCM 和本地 Scraper；在线平台由单独的在线模块构建。

自动发现仅检查既定目录中的固定程序名称，不搜索 PATH 或执行任意目录内容。优先使用每个平台的独立程序；缺失时使用同目录的在线统一程序和固定平台参数补位，不产生重复实例。旧配置使用 `musicfs-plugins --plugin netease|qqmusic|applemusic` 时，可迁到同目录对应独立程序，或在独立程序缺失时使用在线统一程序；保留实例 UUID、开关、权限及账号状态。明确指定的外部程序保持原配置。

已有非空 SHA-256 固定摘要不能被自动清除或替换：旧统一程序的摘要不能证明新独立程序可信，此种配置需显式更新对应摘要。新程序缺失或尚未完成可信配置时不能宣告可用。账号数据保持由 Server 托管，升级不导出凭据、不重新创建实例。

发布脚本记录各模块依赖及许可；模块化构建不改变管理员选择可信程序、校验摘要、受控进程和实例隔离的约束。SQLite、DSP 与其他角色的专用协议没有因此变成通用外部 SDK。

本次验证范围、提交快照检查与限制见 [SDK 拆分验收](validation/plugin-sdk.md)。
