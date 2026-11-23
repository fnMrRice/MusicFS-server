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

官方插件在[版本声明](../../sdk/bundledplugin/releases.json)中登记初始版本和实现文件，实际发布版本由 Server 独立计算。首个[发布基点](../../scripts/internal/pluginversions/release-baseline.json)是提交 `2d5656e5`，包含当时已发布的全部 31 个插件版本。新插件首次发布采用声明的初始版本。

Server 第一父历史中，标题严格符合 `chore(release): publish v0.<minor>.<patch>` 的提交结束一个发布批次。十进制数字除零本身外不带前导零，提交中 `src/core/version/version.go` 的 `Server` 常量须与标题一致。发布提交的构建和此前针对同一批改动的构建得到相同的插件版本。未发布的 HEAD 展示当前批次的候选版本，重复构建、暂存或修改工作树均不推进版本。

每个插件在一批中最多升级一次。构建沿第一父历史检查每个提交实际修改的实现文件，对每个插件取相关提交的最高类别。`feat`、类型或作用域后的 `!`、正文中单列的 `BREAKING CHANGE:` 或 `BREAKING CHANGES:` 使次版本加一并将修订号清零。`fix`、`perf`、`refactor`、`revert` 使修订号加一。纯文档和测试不参与计算。主线上已提交的功能即使随后撤回，仍参与当批计算。共享实现可以影响多个插件。带类型的合并使用其提交说明，默认合并按相对第一父实际合入的文件追溯侧支类型。例如从 `0.2.3` 开始，同批两次 `fix` 得到 `0.2.4`，该批再加入一次 `feat` 则候选版本变为 `0.3.0`。以 `0.3.0` 发布后，下一批的一次或多次修复才会得到 `0.3.1`。

构建检出须包含首个基点至 HEAD 的第一父历史，以及推断默认合并类型所需的侧支历史。Server 和各 Worker 的构建脚本将同一组插件版本注入程序。直接用 Go 调试时，在 Server 根目录运行 `go run ./scripts/pluginversions` 取得链接参数，再通过 `go run -ldflags="<输出的链接参数>" ./src/app/musicfs` 启动。安装包的 `release.json` 记录各官方插件的实际发行版本。

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
