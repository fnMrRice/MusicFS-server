# Go 插件 SDK 与在线插件模块

SDK 和在线插件由私有 Server 仓库维护，具有独立 `go.mod`、构建入口和测试；尚未发布独立模块标签。模块化不扩大平台业务能力或真实账号验收范围。

## 模块与依赖

插件实现按用途位于 `plugins/source`、`plugins/database`、`plugins/decoder`、`plugins/scraper`、`plugins/dsp`、`plugins/analysis`、`plugins/tags`、`plugins/matching` 与 `plugins/transform`。完整目录与模块边界见 [插件目录](../../plugins/README.md)。在线模块为 `plugins/source/online`，本地音乐源为 `plugins/source/localfiles`；数据库插件为 `plugins/database/sqlite`。

| 模块 | 负责内容 | 允许依赖 |
| --- | --- | --- |
| 根 Server 模块 | Server 宿主、编译内置插件与 `plugins/unified` 可选外部入口 | 组装层依赖 SDK 和已知内置实现，包括在线模块 |
| `sdk` | JSON-RPC 帧与取消、协议 DTO、通用错误、实例 ID、在线结果校验、Provider Worker | 标准库及 SDK 明确列出的基础库 |
| `plugins/source/online` | 网易云、QQ 音乐、Apple Music、Bilibili 适配、四个独立程序与统一在线入口 | SDK 和插件自身代码 |

Server 的受信任组装层直接链接官方在线平台适配器，以便在主进程运行；业务代码通过 SDK DTO 和宿主接口使用结果。SDK 和独立在线模块仍不能反向导入 `src/`。`plugins/unified` 保留外部进程兼容入口。公共 DTO 与上游请求实现分开，`Track`、`AccountView` 等类型仍由 SDK 拥有。

来源插件必须在 Manifest 声明 `sourceType: local|network`，其他角色禁止该字段；声明不属于可修改的部署配置。网络音频通过可选 `provider.AudioClient` 提供格式描述和按需读取；`online.audioDescribe` 不读取媒体正文，`online.audioOpen/Read/Close` 在播放时消费有界音频块。宿主按类型选择路径并独立检查 `stream.resolve`，不按平台名称推断格式或播放能力。当前实现范围见 [Bilibili 来源](../providers/bilibili.md)，可选发行建议见[插件发行](optional-distribution.md)。

SDK 包入口：

- `sdk/pluginsettings`：声明式设置页字段、按钮与可选本地化文案，宿主路由及持久化见[插件设置](settings.md)。
- `sdk/audioformat`：插件提供格式探测、识别优先级、解码能力与独立有界容器附件，宿主组合多层解码链并保留各层附件及生命周期；输出格式、终止和资源限制见[音频编解码插件](audio-codecs.md)。
- `sdk/pluginproto`：现有 Provider 协议、Manifest、双向 RPC、帧处理、取消及配置声明校验。
- `sdk/inprocess`：为受信任编译内函数创建独立内存双向管道，取消时关闭阻塞读写；`Peer.Wait` 等待请求退出。
- `sdk/tagformat`、`sdk/contenttransform`：FLAC 标签处理与封面变换的最小能力契约，宿主控制授权和最终发布。
- `sdk/online`：在线平台公共 DTO 与现有官方来源的结果校验策略，不访问平台网络。
- `sdk/provider`：通过接口和工厂注入业务实现的在线 Worker；处理握手、作用域、截止时间、取消、消息大小和并发限制。
- `sdk/fault`：公共错误和随机请求标识。Server `domain.Error` 为同一类型的别名，保持 `errors.As` 行为。
- `sdk/instanceid`：规范实例 UUID 和已有实例路径兼容规则。
- `sdk/bundledplugin`：随包插件各自的发行版本声明，供 Worker 和宿主的未启动元数据共用。

在线平台共用一个插件模块维护公共代码。默认由 Server 固定选择已编译的平台实现，在主进程为每个用户和实例创建独立会话，平台选择必须与握手身份一致。可选的 `musicfs-plugin-netease`、`musicfs-plugin-qqmusic`、`musicfs-plugin-applemusic`、`musicfs-plugin-bilibili` 及 `musicfs-plugin-online --plugin <平台>` 保留独立构建与进程部署能力。SDK 不拥有平台登录、加密、HTTP 请求、Cookie 或平台显示名称。已知平台 ID、结果 URL 校验及最大允许能力仍是 SDK/宿主的官方来源策略，未开放任意第三方身份、未知角色或音频能力。

## 内容类型与 CUE 增量

`sdk/contenttype` 提供已校验的内容类型声明，音乐和 ASMR 内置实现位于 `plugins/content/music`、`plugins/content/asmr`，由启动装配注入只读注册表。当前仅声明名称、角色、外部编号命名空间与固定作品视图；通过独立进程内接口接入，不扩展通用 Manifest 角色，也不提供任意第三方动态加载。作品资料、存储和HTTP语义见[作品资料](../library/works.md)。

`sdk/cuesheet` 是无文件访问的纯文本解析包，输出相对文件引用和每秒75帧的音轨范围；消费方仍需授权与实际资产／时长验证。解析支持与间隙规则见[解析说明](../../sdk/cuesheet/README.md)，尚未接入扫描或分段播放。

## 构建与测试

默认使用根目录 `scripts/build.ps1` 或 `scripts/build.sh` 构建 `musicfs` 与保留十种兼容入口的 `musicfs-plugins`。默认运行只启动 `musicfs`，内置插件不需要后一个文件。所有模块仍以 `GOWORK=off` 构建，不依赖开发者本机的 `go.work`。显式指定 PowerShell `-SplitPlugins` 或 shell `--split-plugins` 才额外生成平台、SQLite、DSP 等独立文件；发布脚本支持同名选项。源码模块、交付文件数量与运行时进程数量分别管理。

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

所有插件必须声明发行版本，格式为最多 128 字符的有效语义版本（SemVer）；缺失、空白、无效格式及带前导零的纯数字预发行标识均拒绝。通用 Manifest 的 `pluginVersion` 由 `ValidPluginVersion` 统一校验，适用于 Provider、音频和 Scraper。发行版本独立于 Server 版本、SDK 模块依赖版本和通信协议；在线插件不要求精确等于 `0.1.0`，通信兼容仍由 `protocolVersion=1.0` 和 `schemaRevision=online-authenticated-v3` 严格判断。版本号变化不会自动获得新方法或权限；FFmpeg 等角色已有的精确版本约束仍保留。

随包七种普通插件、DSP gain、SQLite 和新增的分析、标签、评分、缩略图四项实现的发行版本统一定义在 [sdk/bundledplugin/releases.go](../../sdk/bundledplugin/releases.go)，各插件独立演进。普通 Worker 和管理快照引用同一声明；`GET /admin/plugins` 的每项 `version` 必填，尚无有效握手版本时返回随包声明，包括未启动、待配置、已禁用或启动失败的情况；取得有效握手版本后优先返回实际版本。当前配置仅支持已声明的官方普通插件，未知 ID 仍被拒绝，不以 Server 版本或占位字符串代替插件版本。

在线声明可以提供已知且已验证能力的子集。宿主分别验证 Manifest 与描述，并核对启用能力集合和显示身份一致；重复能力、未知能力、超出当前平台策略的能力、错误协议或声明不一致均拒绝启动。运行期间依据已验证声明展示与执行能力，不能用宿主默认能力覆盖插件自己的限制。新增上游能力仍须更新并验证宿主策略及协议，而不是通过提升插件版本绕过检查。

账号与实例身份保持不变。Web 与 Client 继续读取 Server 返回的能力和必填发行版本；旧 Server 缺失版本时的界面兼容提示不替代服务端校验。独立 DSP 和持久化 Worker 的 ready 响应也必须携带有效 `pluginVersion`；旧 Worker 缺少该字段会被新宿主拒绝。Server、`musicfs-plugins` 及使用的独立入口须配套升级并更新固定摘要，不能只依据未变化的帧协议号或接口合同号判断可混用。专用协议和升级边界见 [DSP 插件](dsp-plugins.md)与[存储插件](storage-plugins.md)。

DSP 配置新增 `builtin` 布尔字段，只允许已编译的 `musicfs.dsp.gain`；相应音频实现声明 `audio.dsp.builtin`。内置 DSP 要求 FFmpeg 同样在 Server 主进程执行，显式外部 FFmpeg 与内置 DSP 的组合在初始化和任务启动时直接拒绝。宿主不会向旧外部 Worker 发送含内置 DSP 的请求；内置握手仍核验上述能力声明。未使用该新能力时仍可使用原有外部 DSP 配置。JSON Schema 同时验证内置名称白名单与外部模式所需的程序摘要。

关闭内置实例时，控制协议等待已取消请求退出，数据通道等待已开始的宿主读写完成后才释放资源和槽位。实现须协作响应取消，不得遗留操作宿主资源的后台任务。与其他 Server 代码一致，内置插件使用错误返回表达可预期失败；未恢复的 panic 会影响主进程，SDK 不提供独立进程的故障隔离。

## 分发与旧配置

默认安装包包含 Server 和兼容工具 `musicfs-plugins` 两个程序，后者默认不启动。`plugins/unified` 保留 `local`、`audio-ffmpeg`、`ncm`、`scraper-local`、`netease`、`qqmusic`、`applemusic`、`storage-sqlite`、`dsp-gain` 九种进程入口。默认内置模式保留各自协议和实例隔离；SQLite 仍是必需组件，DSP 仍按配置启用，不进入普通来源发现列表。新增四类能力没有新增独立程序。

普通插件默认 `execution: builtin`，从编译内目录补齐官方插件，发现不依赖可执行文件。旧配置没有执行模式时也采用内置实现，保留实例 UUID、开关、权限及账号状态。旧路径、参数和摘要可以保留在配置中，但内置模式不会执行这些文件。只有显式 `execution: process` 才走已有可执行文件、参数、摘要验证和进程管理逻辑；未知 ID 不会动态加载到主进程。

发布配置的数据库后端为 `sqlite`，不要求插件可执行文件；已知的旧随包 SQLite 进程配置会在载入时映射成内置后端，显式 `storage.execution: process` 保留外部部署选择。官方 DSP gain 配置映射成内置实现，第三方 DSP 仍使用外部进程和摘要核验。内置插件随 Server 一起升级；外部程序升级须显式更新相应 SHA-256。账号数据保持由 Server 托管，升级不导出凭据、不重新创建实例。

发布脚本记录各模块依赖、许可、内置执行方式与可选外部入口。进程内插件与 Server 共享地址空间，只适用于受信任的编译内实现；SQLite、DSP 与其他角色的专用协议没有因此变成通用外部 SDK。
