# 基础设施插件列表验证

日期：2026-10-21。平台：Windows amd64。以下结果针对当前工作树，尚未提交或发布。

- `go test ./tests/unit/plugincontrol ./tests/integration/plugincontrol ./tests/contract/api` 通过。覆盖默认 SQLite 必需、基础设施无普通实例与不可修改状态；普通插件保留开关；DSP 未配置、已注册未入链、内建和独立进程混合重复节点、完整原链位置、参数独立副本；外部数据库真实发行号、内存可用性、拒绝传输适配器版本，以及 HTTP/JSON Schema 契约。
- `TestExternalStorageAndDSPServerRestartAndPortableRestore` 使用真实 FFmpeg/ffprobe 6.1.1 通过。管理快照包含外部数据库及官方 gain，显示必需、执行模式、链节点与参数，启停修改被拒绝；扫描、播放、配置保存、重启和备份恢复仍完成。
- `TestDSPActualPlaybackCacheSeekRecoveryAndFingerprintIsolation` 使用真实 FFmpeg/ffprobe 6.1.1 通过，新增断言验证处理器保留初始化握手的 DSP 发行版本，未使用的插件不产生虚构版本；原播放、缓存、seek、恢复及指纹隔离断言保持通过。
- `go test ./tests/integration/bootstrap` 和 `TestBundledPluginsAutostartPendingAndPersistedDisable` 通过。默认目录展示七种普通插件和两个基础设施条目，普通实例自动启动、待配置状态及保存停用后的重启语义保持。
- 安全投影检查覆盖数据库选项、进程参数、程序路径和摘要不进入管理列表。gain 行只包含官方内建/外部 gain 节点，不把第三方节点归为官方实现。

`dsp.chainState` 仅说明本次启动配置，不能证明某一播放当前执行了 DSP。当前未提供任意第三方 DSP 的完整管理目录，也未新增配置编辑、商店、安装或升级入口。
