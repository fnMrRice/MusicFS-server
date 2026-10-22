# 在线音乐插件

这是独立的 Go 模块，源码、依赖和测试与 Server 主模块分离；当前仍由同一私有仓库管理。SDK 位于 `../../../sdk`，此处不依赖 Server 的 `src` 包。单个平台可独立编译、测试和替换可执行文件。

默认安装包由根模块的 `plugins/unified` 将本模块与其他用途插件装配成 `musicfs-plugins`，例如 `musicfs-plugins --plugin netease`。下述仅在线统一入口和单平台入口是可选构建，不再默认产生额外文件。

## 目录边界

- `netease/driver`：网易云账号、EAPI 请求、搜索、实体详情、歌单与批次元数据。
- `qqmusic/driver`：QQ 登录流程、桌面 RPC 与搜索映射。
- `applemusic/driver`：MusicKit 用户令牌验证。
- `internal/adapter`：各插件共享的账号隔离、取消、HTTP 限制、限流与分页游标运行时。
- `internal/session`：插件私有账号状态与运行时、平台驱动之间的接口。
- `unified`：统一在线程序的平台选择与参数校验。
- `cmd/musicfs-plugin-online`：统一在线程序，包含三个驱动，每个进程只运行选定的平台。
- `cmd/musicfs-plugin-netease`、`musicfs-plugin-qqmusic`、`musicfs-plugin-applemusic`：三个独立程序，各自只链接本平台的驱动。
- `tests`、`testkit`：插件自己的回归用例和上游模拟数据。

共享运行时通过接口接收平台驱动，不导入任何平台实现。通用 stdio Worker 由 `sdk/provider` 提供，平台包注入客户端工厂、版本和描述符；SDK 不导入本模块。

模块根目录的 `New`、`Serve` 等函数是测试与受信组合入口，会聚合三个平台。统一在线程序复用这层装配；三个独立程序使用各自的平台入口，保持平台依赖隔离。

## 构建与验证

在此模块目录运行：

```powershell
$env:GOWORK = 'off'
go build -o musicfs-plugin-online.exe ./cmd/musicfs-plugin-online
go build -o musicfs-plugin-netease.exe ./cmd/musicfs-plugin-netease
go build -o musicfs-plugin-qqmusic.exe ./cmd/musicfs-plugin-qqmusic
go build -o musicfs-plugin-applemusic.exe ./cmd/musicfs-plugin-applemusic
go test ./...
go vet ./...
```

统一在线程序使用 `--plugin netease`、`--plugin qqmusic` 或 `--plugin applemusic` 选择平台，也支持 `--plugin=netease` 形式。缺失、未知、重复或额外参数均退出并返回状态码 2，错误仅写入标准错误流。

三个独立平台程序均不接受命令行参数，平台在程序构建时确定。两种交付形式都通过 SDK 的 stdio 协议绑定用户、实例、nonce 和超时；每次进程只服务一个选定平台，握手和业务请求不能切换平台。

当前 `replace` 指向同一检出的 SDK，便于离线联调。将此模块迁至独立私有仓库时，需把 SDK 版本替换为已发布且兼容的私有模块版本，并移除本地 `replace`。

## 能力与限制

所有业务请求要求本实例账号已校验，并遵守授权、分页、取消和请求限制。账号及部分平台请求已有模拟回归测试；真实账号验证仍待完成。QQ、Apple Music 未经验证的业务能力继续在描述符中标为不支持；三个插件均不解析完整音频资源。

网易来源提供 `track.details`：通过已授权的歌曲详情请求返回歌曲名、作者 / 艺人、专辑、时长、平台提供的别名与译名，以及平台歌曲 ID 和公开歌曲页面。详情的分组、字段标签和顺序由网易驱动定义；未提供的可选字段省略。详情不会返回登录凭据、短期播放地址或推测音频规格。QQ、Apple Music 尚未实现歌曲详情，不声明此能力。

测试默认使用模拟传输；真实平台登录检查需显式设置现有测试开关。测试包含本地 TLS 服务，运行环境需要允许回环连接。
