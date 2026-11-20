# 开发与检查

在 Server 根目录使用 [go.mod](../../go.mod) 指定的 Go 工具链。仓库包含三个 Go 模块，通过本地 `replace` 关联。

| 模块 | 职责 |
| --- | --- |
| [Server](../../src/README.md) | 服务入口、HTTP API、曲库、音频交付、存储接口及编译内插件 |
| [SDK](../../sdk/README.md) | 公共协议、数据模型、Worker 与插件接口 |
| [在线音乐来源](../../plugins/source/online/README.md) | 平台驱动及测试，仅依赖 SDK |

## 构建与测试

Windows PowerShell：

```powershell
./scripts/build.ps1
./scripts/check.ps1
```

Linux：

```sh
sh scripts/build.sh
sh scripts/check.sh
```

构建默认生成 `musicfs` 与显式外部插件模式使用的 `musicfs-plugins`。需要独立插件程序时，可添加 `-SplitPlugins` 或 `--split-plugins`。检查脚本以 `GOWORK=off` 覆盖三个模块；单独执行 `go test ./...` 只覆盖当前模块。[部署与制品](../operations/deployment.md)

官方插件在[版本声明](../../sdk/bundledplugin/releases.json)中登记初始版本和实现文件。新增插件从 `0.1.0` 开始。构建脚本与发行打包沿 Git 第一父提交历史计算每个插件的小版本号。例如，同一提交修改某个插件的多个实现文件会使其从 `0.1.0` 升为 `0.2.0`。共享文件的修改会递增引用它的各插件版本，单独修改文档或测试则保持版本。增加独立目录或拆分共用文件时应同步维护声明。

开发构建把相对于 HEAD 的全部实现改动合计为一次更新。同一提交和工作树反复构建得到相同版本。发行检出需要包含首次登记版本的提交历史。使用 Go 命令直接调试时，可在 Server 根目录运行 `go run ./scripts/pluginversions` 取得链接参数，再通过 `go run -ldflags="<输出的链接参数>" ./src/app/musicfs` 启动。

宿主测试在 [tests](../../tests/) 下按 `unit`、`contract`、`integration`、`e2e` 划分，辅助程序与数据在 `testkit`、`testapps`、`testdata`。SDK 与在线来源在各自模块内测试。实音频测试按需配置 `MUSICFS_FFMPEG`、`MUSICFS_FFPROBE`，指纹测试另需 `MUSICFS_FPCALC`。真实账号开关和测试素材见[复验方法](../validation/verification.md)与[夹具说明](../validation/fixtures.md)。

构建后可生成宿主测试覆盖率。竞争检查需支持的平台及本机 C 编译器。

```powershell
go test -coverpkg=./src/...,./plugins/... -coverprofile=build/coverage.out ./tests/...
$env:CGO_ENABLED = '1'
./scripts/check.ps1 -Race
```

## 协议与数据库

[协议目录](../contracts/README.md)维护 OpenAPI、事件与插件 Schema。改动时核对 SDK、客户端和[总体架构](https://github.com/fnMrRice/MusicFS/blob/main/docs/architecture.md)。主库和实例库的结构由 SQLite 插件维护，结构变化以 [SQL 迁移](../../plugins/database/sqlite/migrations/README.md)交付。

在完整 MusicFS 工作区中运行 `python scripts/check-docs.py` 可检查文档导航、相对链接及跨仓库引用。其他开发约定见[仓库说明](../../AGENTS.md)。
