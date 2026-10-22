# MusicFS 插件 SDK

独立 Go 模块：`github.com/fnMrRice/MusicFS-server/sdk`。协议和 SDK 由
Server 仓库维护。独立在线插件及通用 SDK 消费者只依赖此模块；部分编译内算法和 SQLite 后端仍使用宿主拥有的业务契约，不承诺所有插件都已成为独立 SDK 模块。

## 模块边界

- `contenttype`：作品内容类型的角色、编号命名空间与固定视图声明；宿主负责注册、资料保存与查询。
- `cuesheet`：CUE 纯文本解析与精确帧范围，无文件访问；支持范围见[说明](cuesheet/README.md)，不代表已接入导入或播放。
- `pluginsettings`：插件设置页的文本、数字、复选、选择字段与动作按钮声明，支持可选本地化文案；宿主校验与持久化见[声明式设置契约](../docs/plugins/settings.md)。
- `audioformat`：编解码插件的受限格式探测、优先级与注册顺序、通用元数据及多层解码链，含显式终止、资源清理和大小/深度限制；见[音频编解码插件](../docs/plugins/audio-codecs.md)。
- `pluginproto`：JSON-RPC 消息帧、双向请求、握手、插件清单、导入配置与扫描协议。
- `provider`：在线 Provider 接口和 Worker 运行入口；由插件注入客户端工厂与展示信息。
- `inprocess`：为受信任编译内实现建立独立上下文及双向内存流，取消时关闭阻塞读写并等待处理退出。
- `tagformat`：标签补丁、音频字节范围及重写接口；宿主负责授权、校验、原件替换与恢复。
- `contenttransform`：有界缩略图接口；宿主负责对象权限、缓存、父对象版本与发布。
- `online`：公共 DTO、平台标识、能力策略、元数据摘要与宿主结果校验。
- `fault`：公共错误类型与请求 ID；宿主 `domain.Error` 是其类型别名。
- `instanceid`：实例 UUID 与兼容旧实例目录的身份规则。
- `bundledplugin`：随包插件共用的发行版本声明。

所有官方内置插件在 Server 主进程运行，现有 Provider/Scraper/音频/DSP 协议可使用内存流，SQLite 直接调用后端接口。显式外部进程模式才使用独立程序和 stdio；外部工具 FFmpeg、ffprobe、fpcalc 仍按任务执行。账号、用户及 UUID 实例状态继续隔离；共享进程不提供崩溃隔离或安全强停，取消必须协作。

新增插件当前仅实现 `plugins/analysis/chromaprint` 的指纹、`plugins/tags/flac` 的 FLAC 标签、`plugins/matching/standard` 的标准评分及 `plugins/transform/thumbnail` 的 PNG/JPEG 缩略图。指纹与评分契约仍分别由宿主 `fingerprintworker.Extractor`、`candidates.Scorer` 拥有。接口存在不表示已支持 BPM/调性、更多标签格式、歌词对齐或任意第三方安装。

SDK 不包含 QQ 音乐、网易云音乐的请求、登录、凭据或 HTTP 客户端实现。
`online` 中保留官方平台已验证的能力上限、来源链接和图片引用校验规则，
未知第三方平台未开放；实际平台能力见[在线插件](../plugins/source/online/README.md)。

## 版本与兼容

插件发布版本使用语义版本，例如 `1.2.3`、`2.0.0-rc.1+build.5`，
不要求等于 `0.1.0`。插件版本、协议版本、配置版本分别校验：
只有匹配 `ProtocolVersion`、`OnlineRevision`、身份、权限和资源限制的插件才能接入。
插件发行号不要求和 Server 版本号一致。内置版本由 `bundledplugin` 独立声明，升级实现需要重新构建和交付 Server；外部插件在已支持协议内升级，不依赖修改宿主的固定发行号，也不能借版本号绕过身份与能力校验。

插件可以声明宿主已验证能力的子集，不能启用未验证能力。宿主必须在握手后调用
`pluginproto.ValidateOnlineDescriptor(manifest, descriptor, platform)`，
核对清单与描述符的展示身份和实际启用能力集合；不能只分别验证两份结构。
客户端应根据这次握手的实际能力显示功能。
Worker 也会根据插件描述符拒绝未启用的业务方法；账户管理和插件描述信息始终保留为控制接口。
未启用能力的请求仍先确认账户状态，未登录时保持 `PLATFORM_AUTH_REQUIRED`，
已登录时返回 `UNSUPPORTED`；支持的能力由具体实现执行原有账户授权。
只实现部分能力的插件可以嵌入 `provider.UnimplementedClient`，再覆盖已实现的方法。

`online.Digest` 与 `online.PlaylistDigest` 是元数据观测摘要，
不代表音频字节版本或播放授权。共享校验只验证结果结构，账户权限仍由宿主和插件共同执行。

## 本地开发与验证

在 `server/sdk` 目录执行：

```powershell
$env:GOWORK = 'off'
go test ./...
go vet ./...
go build ./examples/metadata
```

同一检出内由各模块的 `replace` 指向本地 SDK；外部私有插件仓库应依赖发布后的
`sdk/vX.Y.Z` 模块标签，并配置其私有模块访问权限。尚未发布 SDK 标签时，可在消费者的
`go.mod` 中临时使用 `replace github.com/fnMrRice/MusicFS-server/sdk => <本地 SDK 目录>`。
不要把凭据写进模块文件或仓库。

`examples/metadata` 是仅依赖 SDK 的离线示例，返回固定测试元数据，不访问真实平台，
不提供真实账户登录，也不应随生产安装包发布。示例沿用官方网易云协议标识以验证现有协议；
它不代表第三方平台已被允许，也不代表歌曲 `42` 的真实信息。
