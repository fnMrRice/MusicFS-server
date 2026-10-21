# MusicFS 插件 SDK

独立 Go 模块：`github.com/fnMrRice/MusicFS-server/sdk`。协议和 SDK 由
Server 仓库维护；插件只依赖此模块，不需要导入 Server 的 `src` 包。

## 模块边界

- `pluginproto`：JSON-RPC 消息帧、双向请求、握手、插件清单、导入配置与扫描协议。
- `provider`：在线 Provider 接口和 Worker 运行入口；由插件注入客户端工厂与展示信息。
- `online`：公共 DTO、平台标识、能力策略、元数据摘要与宿主结果校验。
- `fault`：公共错误类型与请求 ID；宿主 `domain.Error` 是其类型别名。
- `instanceid`：实例 UUID 与兼容旧实例目录的身份规则。
- `bundledplugin`：随包插件共用的发行版本声明。

SDK 不包含 QQ 音乐、网易云音乐的请求、登录、凭据或 HTTP 客户端实现。
`online` 中保留官方平台已验证的能力上限、来源链接和图片引用校验规则，
未知第三方平台未开放；实际平台能力见[在线插件](../plugins/source/online/README.md)。

## 版本与兼容

插件发布版本使用语义版本，例如 `1.2.3`、`2.0.0-rc.1+build.5`，
不要求等于 `0.1.0`。插件版本、协议版本、配置版本分别校验：
只有匹配 `ProtocolVersion`、`OnlineRevision`、身份、权限和资源限制的插件才能接入。
发布新插件版本不要求 Server 同步改版本常量。

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
