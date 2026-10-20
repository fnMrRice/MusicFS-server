# SDK、插件分类与在线统一入口验收

日期：2026-10-20。范围为独立 Go SDK、插件用途分类、在线平台独立/统一程序、宿主接入及构建分发；不扩大平台真实账号、上游 API 或完整音频的验收结论。

## 验收对象

- 根 Server、`sdk`、`plugins/source/online` 三个模块，均以 `GOWORK=off` 验证。
- 插件按 `source`、`database`、`decoder`、`scraper`、`dsp` 分类；可执行入口随对应插件移动。
- 在线独立入口各自只依赖所属平台驱动；`online/unified` 装配三平台，使用 `musicfs-plugin-online --plugin <平台>`，每个进程固定一个平台。
- 宿主生产依赖不包含在线实现，SDK/在线插件不依赖宿主 `src`；真实依赖图测试验证此边界。
- 独立发布版本、有界语义版本、声明能力子集、Manifest/Descriptor 一致性和登录优先行为均有回归覆盖。

工作区存在另一个任务的账号归属及管理展示修改。本次额外导出待提交的 Git 暂存区，在 `build/sdk-commit-snapshot-20261020` 验证；以下提交快照测试不依赖那些未暂存代码。原工作区修改予以保留。

## 已通过检查

| 检查 | 命令或覆盖范围 | 结果 |
| --- | --- | --- |
| SDK | 模块内 `go test -mod=readonly ./...`、`go vet -mod=readonly ./...` | 通过，含示例编译 |
| 在线插件 | 模块内 `go test -mod=readonly ./...`、`go vet -mod=readonly ./...` | 通过，含三平台夹具、Worker 与 unified |
| 宿主与分类插件 | `go test -mod=readonly -count=1 -timeout=10m ./src/... ./plugins/... ./tests/unit/... ./tests/contract/... ./tests/integration/plugins` | 提交快照完整通过 |
| 静态检查 | 根模块 `go vet -mod=readonly ./src/... ./plugins/... ./tests/... ./scripts/release`，Go 格式及暂存差异检查 | 通过 |
| 发布及配置 | 配置全测试、发布依赖/许可/清单测试 | 通过 |
| 平台分发 | Windows amd64 八程序发布，Linux amd64 八程序交叉编译 | 通过；Linux 未执行运行验收 |
| 安装发现 | 自动启动、待配置、显式禁用持久化 | 通过 |
| 安装包在线入口 | 网易云、QQ、Apple 各自独立及 unified，共六种真实进程握手与退出 | 通过，使用本地状态 |

发现与迁移测试覆盖：独立程序优先、缺失时统一入口补位、两种入口共存不重复创建实例、保留显式选择、固定摘要不被清除、保留自定义参数、旧配置的实例 UUID/开关/权限，以及 `--plugin value` 和 `--plugin=value` 两种合法选择形式。错误平台和额外参数不能进入就绪状态。

## 本地证据与限制

提交快照测试日志保存在忽略的构建目录：

- `build/sdk-commit-host-tests.log`
- `build/sdk-commit-sdk-tests.log`
- `build/sdk-commit-online-tests.log`
- `build/sdk-commit-vet.log`

工作区八程序分发检查另有 `build/sdk-eight-config-release-tests.log`、`build/sdk-eight-windows-release.log`、`build/sdk-eight-linux-build.log`、`build/sdk-eight-installation-online-e2e.log`；等号选择参数修复的配置回归见 `build/sdk-selector-config-tests.log`。这些日志和二进制不是 Git 交付文件。

提交快照也已重新生成完整 Windows 八程序安装包，并通过三平台独立/统一入口的六项实际安装测试。快照内日志为 `build/sdk-commit-windows-release.log` 和 `build/sdk-commit-installation-online-e2e.log`，制品位于快照的 `build/releases/sdk-commit-windows-20261020`。清单标记为提交前的本地构建，不将其当作已发布版本。

Windows 凭据保护、本机 TLS、进程启动及目录连接相关测试在正常本机权限下执行。缺少 FFmpeg、ffprobe 或指纹工具时，原有真实媒体工具测试保留显式跳过；本次不把编译及协议测试视为媒体播放或平台外网验证。未执行 Linux 运行、真实平台账号登录和线上升级，不创建远程模块标签或新仓库；模块仍使用仓库内 `replace`。
