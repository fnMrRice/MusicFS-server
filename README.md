# MusicFS Server

简体中文 · [English](docs/README.en.md)

MusicFS 的自托管音乐服务。它扫描服务器上的音乐目录，把不同文件版本、歌词和封面整理进同一个曲库，并通过 HTTP 向 Web 和原生客户端提供音频。

- **整理曲库**　按曲目、专辑、歌手或文件夹浏览，管理收藏与歌单。可导入 CD 专辑、ASMR 和电台作品包。
- **补全资料**　读取内嵌与旁置歌词、封面，也可查询在线资料，预览后按字段选用。[刮削与在线插件的资料来源](docs/plugins/provider-api-sources.md)
- **播放音频**　按客户端支持的格式交付原流或通过 FFmpeg 转码。Bilibili 来源支持按链接读取公开音轨、收藏夹及音频歌单。其他在线平台的账号和资料能力见[在线来源](docs/providers/online.md)。

## 本地启动

以下命令在 Server 仓库根目录执行，使用 [go.mod](go.mod) 指定的 Go 1.26.9 工具链。Windows PowerShell 示例启动本机 API 服务。

```powershell
./scripts/build.ps1
Copy-Item configs/server.example.yaml configs/server.local.yaml
$env:MUSICFS_ADMIN_TOKEN = (& ./build/windows/musicfs.exe token).Trim()
./build/windows/musicfs.exe serve --config ./configs/server.local.yaml
```

启动后打开[就绪检查](http://127.0.0.1:8080/api/v1/health/ready)。需要浏览器界面时，先构建 [MusicFS Web](https://github.com/fnMrRice/MusicFS-web)，再给 `serve` 加上 `--web-root <dist 目录的绝对路径>`。在启动服务前可用 `$env:MUSICFS_ADMIN_TOKEN` 查看本次登录使用的令牌。登录后在「设置 → 音乐源 → 本地音乐」添加目录即可扫描。Linux 命令、目录授权和 FFmpeg 配置见[本地运行与扫描](docs/operations/running.md)。

## 文档

| 我想了解 | 入口 |
| --- | --- |
| 部署、配置或备份服务 | [部署](docs/operations/deployment.md) · [配置](docs/operations/configuration.md) · [备份与恢复](docs/operations/backup.md) |
| 接入客户端 | [OpenAPI 与事件协议](docs/contracts/README.md) · [Web 接入](docs/contracts/web-integration.md) |
| 开发服务端或插件 | [开发与检查](docs/development/README.md) · [源码导航](src/README.md) · [插件 SDK](sdk/README.md) |
| 查看架构与支持范围 | [Server 架构](docs/architecture.md) · [验证范围](docs/validation/README.md) · [全部文档](docs/README.md) |

本项目几乎完全由 AI 生成。

[MusicFS 项目主页与界面截图](https://github.com/fnMrRice/MusicFS) · [许可证](LICENSE)
