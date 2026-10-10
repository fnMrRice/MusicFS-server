# YAML 配置层与读写服务验收

日期：2026-10-09。本轮范围为配置模型、严格YAML读取、显式插件/实例开关、带修订的配置读写与保存恢复；动态启停、管理API/命令及排空租约不在本轮实现范围。当前用法见[配置说明](../configuration.md)，不兼容旧JSON配置。

## 已完成证据

| 检查 | 结果与证明范围 |
| --- | --- |
| Windows配置读写与崩溃恢复 | 最终全量检查中的 `tests/integration/config` 通过（10.911秒），竞争检查通过（13.177秒）；覆盖注释保留、不回写有效覆盖值、修订冲突、外部编辑/失败重载、只读和无日志拒绝、并发写与重启，以及子进程在替换前后真正退出的保存中断恢复 |
| Windows真实Server YAML E2E | 通过（19.346秒）；配置实际加载及Server启动、扫描、播放路径使用YAML，未将原JSON仅改后缀 |
| Web构建与单测 | `npm run build`（含严格TypeScript）通过，既有23项单测通过 |
| Edge NCM三项回归 | Edge 154.0.4258.62一次3/3通过（20.4秒）：新YAML配置注册并启用Local Files、音频与NCM；MP3全流摘要/精确Range/PCM、FLAC转MP3和4秒定位、损坏NCM后普通WAV恢复 |

浏览器本轮数据见[Web YAML独立证据](../../../web/docs/validation/yaml-config-edge-2026-10-09.json)和[NCM迁移附录](../../../web/docs/ncm-validation.md)。原19.3秒的NCM结果及截图仍是迁移前历史记录，没有覆写。本轮未重复无关完整浏览器套件，非零PCM不代替人工扬声器听音。

## 最终回归

- Windows完整 `scripts/check.ps1 -Race` 通过：格式、静态检查、全部测试及全部竞争检查通过，使用真实FFmpeg/ffprobe；Server E2E普通测试27.794秒，竞争检查24.766秒。
- Windows与Linux的Server、Local Files、音频、NCM四种程序分别构建成功。
- Arch WSL原生运行（2026-10-09历史）：YAML Server E2E通过（40.539秒），配置与启动单测通过（0.061秒、0.007秒），包括真实CLI启动、扫描、多实例/多音乐根、原流Range、NCM及重启。当时没有Linux FFmpeg/ffprobe，音频插件启用边界用例明确跳过；后续补验见下项。
- 2026-10-10补齐Linux FFmpeg/ffprobe 6.1.6：新版YAML真实处理、三段PCM标记、4000ms定位、双层启用边界及同配置重启均通过；E2E、插件集成、配置读写、配置及启动单测的完整相关包回归通过，E2E/配置读写race通过。Windows新增YAML用例race及NCM测试两平台回归也通过。具体命令、依赖快照、测量和工具摘要见[Linux音频记录](configuration-linux-audio.md)。
- Arch WSL在原生Linux临时目录运行配置写入集成测试，全项通过（0.082秒），包含原子替换、权限、并发、外部编辑、失败重载与替换前后崩溃恢复。
- 正式 `configs/server.example.yaml` 同时通过文件加载器、文档解析器及编辑器Schema校验，配置单测最终通过（2.544秒）；示例路径与四程序制品、运行说明一致。38份相关README/文档的本地链接通过检查，旧JSON示例与旧迁移草案路径已移除。
- 最终复核补充 `Local` / `local` 实例ID冲突拒绝，避免Windows状态目录重叠；单个大写ID保持不变。同插件、跨插件的加载器回归均通过；追加配置单测与配置读写集成的竞争检查分别通过（2.821秒、4.708秒），并重新完成四程序双平台构建。

不得从Windows通过或交叉构建推断Linux运行通过；没有执行或被跳过的真实音频测试不算兼容证据。

## 保存与运行应用边界

写入成功只说明期望开关已保存为`saved`，`RestartRequired`保持待应用状态；读写服务尚未接入管理API或动态生命周期。`Reload`只更新有效的已加载配置，`MarkApplied`须由真正完成应用的生命周期所有者调用。当前用户通过停服编辑、重启应用配置。

SQLite日志在替换前保存`prepared`，重启按磁盘修订将操作归为`notSaved`、`saved`或`superseded`，不重新执行旧写入。Linux使用同目录文件/目录同步与rename，Windows复制DACL并用write-through替换；协作`.lock`不防不遵守锁的外部编辑器竞争。只读挂载、缺少日志或不能安全替换时拒绝配置写入。
