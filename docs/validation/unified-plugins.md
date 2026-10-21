# 默认全量统一插件验收

日期：2026-10-20。默认交付从八个程序收敛为 `musicfs` 与 `musicfs-plugins` 两个程序。源码仍按用途分类，SDK 与在线平台仍保留独立模块；本次只改变装配和默认分发，不新增平台能力或更改协议。

## 实现范围

`plugins/unified` 支持九个固定角色：`local`、`audio-ffmpeg`、`ncm`、`scraper-local`、`netease`、`qqmusic`、`applemusic`、`storage-sqlite`、`dsp-gain`。每次进程只运行一个角色；三种 stdio 协议分别沿用既有媒体、持久化和 DSP 契约。

默认构建只产生 Server 和一个插件文件。PowerShell `-SplitPlugins`、Shell `--split-plugins` 显式额外构建六个独立文件；默认构建清理同目标目录中固定名单内的十个旧独立生成文件（包含更早的四种本地独立入口），保留未知文件。默认发布配置通过统一程序的 `storage-sqlite` 角色启动必需 SQLite Worker，DSP 保持可选。

自动发现优先使用全量统一程序，兼容显式独立入口和仅在线统一入口。固定摘要、自定义路径和参数、实例 UUID、归属及启停设置保留。旧四角色程序不能被当作新版程序，必须更新二进制；协议握手失败会报告不可用。SQLite/DSP 使用显式专用配置，不进入普通插件发现列表。

## 已验证

- `go test -mod=readonly -count=1 -timeout=10m ./tests/integration/plugins ./plugins/unified ./src/app/musicfs-plugins` 完整通过。覆盖单程序多平台、非法/重复参数、握手身份不符、依赖边界，以及在线进程关闭后 SQLite 读写和 DSP PCM/时序保持有效。
- `TestBundledPluginsAutostartPendingAndPersistedDisable` 在只有全量统一插件文件时通过，覆盖七个普通插件的发现、待配置状态和重启后的禁用持久化。
- `tests/unit/config` 与 `src/core/config` 全测试通过，覆盖两种选择参数、显式入口保留、已知缺失独立文件回退、固定摘要保护和不重复创建实例。Windows junction 测试在正常本机权限下复验通过。
- 发布测试通过，覆盖默认两个/显式分拆八个程序的清单和组件、统一 SQLite 参数及摘要、模块依赖和许可证收集。
- Windows amd64 双程序安装包生成成功；实际包通过三个在线角色握手、SQLite session 持久化读写和 DSP gain PCM 检查。清单包含两个程序、九个角色、三个源码模块和 35 个依赖，已编译第三方依赖无缺失许可证，SQLite 必需标记正确，完成后无 `.incomplete`。
- Windows 显式分拆构建生成八个程序，随后默认构建回到两个程序，旧本地独立产物被清理，未知文件保持原样。Linux amd64 默认两个程序交叉编译通过；PowerShell 解析和 Shell 语法检查通过。沙箱曾报告 Go stat-cache 写入权限警告，构建退出码均为零。
- 根模块 `go vet -mod=readonly ./src/... ./plugins/... ./tests/... ./scripts/release`、定向格式和差异检查通过。

Windows 验证包位于 `build/releases/unified-two-windows-20261020`，标记为未提交工作区的验证构建。`bin` 两个文件合计 37,304,832 字节（35.58 MiB）；此前同平台、相同裁剪参数的八程序验证包为 79,220,736 字节（75.55 MiB），减少约 52.91%。此处只比较程序文件，不计外部媒体工具和其他安装资源。

本地日志位于忽略的 `build/` 目录：`unified-all-integration.log`、`unified-all-discovery-e2e.log`、`unified-config-host.log`、`unified-two-release-unit.log`、`unified-two-installation-e2e.log`、`unified-two-windows-release.log`、`unified-two-build-switch.log`、`unified-two-linux-build.log`、`unified-all-vet.log`。

本次不进行真实平台登录或外网业务验证，不宣称新增在线整曲能力。需要 FFmpeg、ffprobe 或指纹工具的原有测试继续按工具可用性执行/跳过；Linux 交叉编译不等于 Linux 运行验收。共享一个文件减少交付文件和重复编译内容，不合并实例权限、凭据或运行进程。
