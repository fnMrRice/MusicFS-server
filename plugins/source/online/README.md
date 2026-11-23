# 在线音乐来源

本模块实现四种音乐来源。默认随 Server 在主进程运行，平台驱动共用 [SDK](../../../sdk/README.md) 的握手与消息协议。各项能力随实例声明返回。

| 来源 | 已接入的能力 |
| --- | --- |
| [网易云音乐](netease/driver/) | 账号登录、搜索、曲目详情、歌单、批量元数据及按播放音质读取完整音轨 |
| [QQ 音乐](qqmusic/driver/) | 账号登录、桌面 RPC 与搜索结果映射 |
| [Apple Music](applemusic/driver/) | MusicKit 用户令牌验证 |
| [Bilibili](bilibili/README.md) | 按编号或链接读取公开曲目、视频分 P、收藏夹和音频歌单，按需读取公开音轨 |

前三种来源的业务查询要求当前实例已验证平台账号。Bilibili 使用匿名公开接口。具体协议见[来源与 API 对照](../../../docs/plugins/provider-api-sources.md)，登录和实测范围见[平台账号说明](../../../docs/providers/platform-login.md)。

网易云插件的「播放音质」提供沉浸环绕声、超清母带、高清臻音、高解析度无损、无损、极高和标准，默认选中极高。插件使用已登录账号请求曲目的播放地址。缓存记录以平台返回的实际音质和可用媒体摘要识别版本。平台仅允许试听或拒绝播放时，插件会返回访问错误。曲目和会员档位对应的实际效果仍待实号验收。[网易云播放接口参数参考](https://github.com/NeteaseCloudMusicApiEnhanced/api-enhanced/blob/2db6684453297ee86dec0492842f2320b08182d9/module/song_url_v1.js)

## 代码入口

- [平台适配器](internal/adapter/)负责取消、限流、分页和受限 HTTP；[账号会话](internal/session/)隔离实例状态。
- [平台策略](internal/policies/)声明资源身份、页面链接和图片域名。插件描述中的 `resources` 和可选 `login` 提供客户端可用的资源规则与登录方式。
- [统一入口](unified/)在启动时固定一个平台。[cmd](cmd/)也提供四个单平台入口，各自只链接对应驱动。注册新的工厂需要修改受信任装配并登记宿主准入身份。

## 构建与检查

在本模块目录运行。以下 PowerShell 命令构建可选的外部进程，并使用本地模拟响应执行测试。

```powershell
$env:GOWORK = 'off'
go build -o musicfs-plugin-online.exe ./cmd/musicfs-plugin-online
go test ./...
go vet ./...
```

统一程序用 `--plugin <平台>` 选择 `netease`、`qqmusic`、`applemusic` 或 `bilibili`，例如 `musicfs-plugin-online --plugin bilibili`。外部模式的标准输入输出用于宿主协议。分拆构建与整套插件检查见[开发指南](../../../docs/development/README.md)。
