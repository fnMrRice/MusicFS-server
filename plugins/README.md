# 服务端内置插件

默认只启动 `musicfs`。官方插件与宿主在同一进程运行，每个实例独立持有配置、账号和任务上下文。FFmpeg、ffprobe 和 fpcalc 等工具按任务调用。

| 用途 | 实现 |
| --- | --- |
| 音乐来源 | [本地文件](source/localfiles/) · [网易云音乐、QQ 音乐、Apple Music 和 Bilibili](source/online/README.md) |
| 歌词与资料 | [本地歌词与封面](scraper/local/README.md) · [在线资料插件](scraper/online/) · [来源与公开 API](../docs/plugins/provider-api-sources.md) |
| 作品类型 | [音乐、CD 专辑、ASMR、电台](content/) |
| 编解码与 DSP | [FFmpeg](decoder/ffmpeg/) · [NCM](decoder/ncm/) · [增益、均衡器、压缩器、混响及声道转换](dsp/) |
| 音频分析与匹配 | [Chromaprint 指纹](analysis/chromaprint/) · [标准候选评分](matching/standard/) |
| 媒体处理 | [FLAC 标签重写](tags/flac/) · [PNG/JPEG 缩略图](transform/thumbnail/) |
| 数据库 | [SQLite](database/sqlite/) · [主库迁移](database/sqlite/migrations/README.md) · [实例状态库](database/sqlite/state/) |

## 开发与运行

公共协议由 [SDK](../sdk/README.md) 提供，宿主的注册、调度和授权在 [src/plugins](../src/plugins/)。在线音乐来源有自己的 [Go 模块](source/online/go.mod)，通过 SDK 接入；其余实现随 Server 根模块构建。管理界面的功能分类取自插件的 `features` 声明。

需要显式使用外部进程时，可通过 [unified](unified/) 装配的 `musicfs-plugins --plugin <用途>` 启动相应角色。内置插件升级需重新构建 Server。执行模式、专用协议与发行约束见[插件架构](../docs/plugins/plugin-architecture.md)和[可选插件发行](../docs/plugins/optional-distribution.md)。
