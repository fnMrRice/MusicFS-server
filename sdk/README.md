# MusicFS 插件 SDK

`github.com/fnMrRice/MusicFS-server/sdk` 是独立的 Go 模块。它提供插件握手、能力声明、资源访问协议及音频和元数据接口。平台请求与登录实现位于[在线来源模块](../plugins/source/online/README.md)。

## 选择接口

| 用途 | 包 |
| --- | --- |
| 声明插件与建立连接 | [pluginproto](pluginproto/) 定义消息帧、握手和清单；[inprocess](inprocess/) 为内置实现建立独立上下文与内存流 |
| 在线音乐来源 | [provider](provider/) 提供客户端接口和 Worker 入口；[online](online/) 定义通用资源模型和结果校验 |
| 作品与曲目 | [contenttype](contenttype/) 定义内容类型、视图及排序；[cuesheet](cuesheet/README.md) 把 CUE 文本解析为音轨帧范围 |
| 音频、标签与封面 | [audioformat](audioformat/) 定义格式探测与解码链；[tagformat](tagformat/) 定义标签重写；[contenttransform](contenttransform/) 定义缩略图转换 |
| 资料查询与设置 | [scraper](scraper/) 定义在线资料候选；[pluginsettings](pluginsettings/) 定义配置字段与动作 |
| 身份与诊断 | [instanceid](instanceid/) 管理实例 UUID；[bundledplugin](bundledplugin/) 声明随包版本；[fault](fault/) 提供公共错误 |

插件以 SemVer 声明发行版本。宿主另外校验协议版本、在线描述修订、身份及权限，握手成功后通过 `pluginproto.ValidateOnlineDescriptor` 核对清单与描述符的能力集合。只实现部分方法的来源可嵌入 `provider.UnimplementedClient`。客户端以本次握手的声明显示功能。

`online.Digest` 与 `online.PlaylistDigest` 表示元数据观测摘要。音频字节版本和播放授权各有独立契约。[协议与版本](../docs/plugins/plugin-sdk.md) · [Worker 协议](../docs/contracts/plugins/v1/README.md)

## 构建离线示例

在本目录执行。示例使用固定元数据，只用于演示 SDK 接口。

```powershell
$env:GOWORK = 'off'
go test ./...
go vet ./...
go build ./examples/metadata
```

同一检出中的消费者通过 `go.mod` 的 `replace` 引用本地 SDK。开发独立模块时，可将 `github.com/fnMrRice/MusicFS-server/sdk` 指向本地目录；使用发布版时应以实际存在的 `sdk/vX.Y.Z` 标签固定依赖。[离线示例源码](examples/metadata/)
