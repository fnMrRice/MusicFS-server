# MusicFS-server

MusicFS 的 Go 后端服务。当前已落地 Local Files 扫描、SQLite 音乐库、会话鉴权、原流 Range、独立 Go DSP 处理链、本地 NCM 解密插件、后台完整文件摘要和本地音频指纹，配合独立 Web 仓库提供最简列表播放器。启动步骤见[本地运行说明](docs/running.md)，每步提交与验证边界见[实施记录](docs/validation/progress.md)。

配置已统一为 `configVersion: 1` 的 YAML，插件与实例显式登记和启用；严格解析、修订写入与崩溃恢复见[配置说明](docs/configuration.md)和[配置验收](docs/validation/configuration.md)。旧 JSON 配置不兼容。读写服务只保存期望状态，当前仍需重启应用，尚未提供动态启停管理 API/命令。

长期设计包括（未列入实施记录的能力仍为计划）：

- 插件管理：支持插件及实例的启用/禁用；默认平滑停用，停止接收新任务并等待现有播放结束，另提供强制停止。具体步骤见[插件管理计划](docs/implementation/plugin-management.md)，当前尚未实现完整管理链路。
- 插件化 Music Provider：本地文件、已挂载 NAS 和在线音乐来源；推荐歌单是 Provider 可选能力。
- 独立 Scraper 插件：支持自动刮削及手动搜索、预览和选用。歌词、封面等内容保存在独立文件目录，数据库保存索引与引用；默认不写入原始音频文件，可对授权本地来源配置写回。
- 插件化音频处理：解复用、解码、DSP、编码及封装，向 Web 等客户端交付兼容输出；适用时保留直接播放或代理路径。
- 记录资产码率、完整文件 SHA-512，并以音频指纹辅助匹配；跨端播放与续播行为遵守共同协议。

文档入口：

- [Server 分阶段实施](docs/implementation.md)：P0—P8 的文件职责、逐步任务、测试与完成门槛。
- [YAML 配置](docs/configuration.md)：字段、插件与实例、覆盖顺序、配置读写及应用边界。
- [插件实施计划](docs/implementation/plugins.md)：协议宿主、Local Files、音频链、独立 Scraper 和平台适配。
- [联合实施总览](../docs/implementation.md)与[最小 Web 实施](../web/docs/implementation.md)：共同接口和真实浏览器播放验收。
- [Server 内部架构](docs/architecture.md)：模块、数据模型、Provider/Scraper、存储与任务管理。
- [Server 音频处理](docs/audio-processing.md)：处理链、播放交付、定位、缓存与资源约束。
- [本地音频指纹](docs/fingerprinting.md)：固定工具、后台任务、独立明细文件、恢复和管理接口；默认禁用，不上传外部服务。
- [候选与人工关联](docs/matching.md)：有界比较、排除优先、修订冲突、解除/拆分和可逆审计；保留全部来源。
- [歌词与封面文件](docs/media.md)：有界上传、私有预览、按来源资产选用、人工锁和版本失效。
- [媒体恢复与备份](docs/media-storage.md)：持久发布阶段、引用保护回收和一致性数据库/媒体快照。
- [来源专辑与发行](docs/albums.md)：显式有序成员、独立封面、人工锁、修订及按专辑刮削。
- [派生媒体](docs/derived-media.md)：封面缩略图、完整刮削记录、授权读取与历史备份。
- [公开推荐](docs/recommendations.md)：按本人在线来源聚合、有限缓存、失败保留与刷新；读取不导入或播放。
- [远程歌单](docs/remote-playlists.md)：显式导入、保守同步、固定本地快照、待复核缺失与取消；保留重复出现。
- [个人歌单](docs/playlists.md)：有序组合、本地追加、完整出处、独立隐藏修订和关联扩大确认。
- [内部组件图](docs/diagrams/components.puml)：可编辑的 PlantUML 源码。
- [跨端架构](../docs/architecture.md)：Server 与客户端共同遵守的协议和行为。
- [工作约定](AGENTS.md)：文档维护与仓库协作规则。

本仓库与 Client、Web 分别管理 Git 历史。跨仓库相对链接按 MusicFS 根目录下 `server`、`client`、`web` 并列检出的工作区布局解析。

工程采用根 `go.mod` 的单 module：源码放 `src/`，Server 与插件程序入口放 `src/app/<程序>/`，测试统一放根 `tests/`。使用 `scripts/build.ps1` / `scripts/build.sh` 构建八个独立程序：`musicfs`、`musicfs-plugin-local`、`musicfs-plugin-audio-ffmpeg`、`musicfs-plugin-ncm`、`musicfs-plugin-scraper-local`，以及[在线资料插件](docs/online.md) `musicfs-plugin-netease`、`musicfs-plugin-qqmusic`、`musicfs-plugin-applemusic`；使用 `scripts/check.ps1` / `scripts/check.sh` 检查。真实音频工具测试需设置 `MUSICFS_FFMPEG` 和 `MUSICFS_FFPROBE`；指纹测试另需 `MUSICFS_FPCALC`，缺少工具时明确跳过对应部分。

第一条交付链为 Local Files 扫描 → 音乐库列表 → 授权原流 → Web 有声播放；随后补齐解码 → PCM DSP → 编码/封装 → Web 播放和时间定位。P5—P7 的匹配、刮削及在线来源逐阶段扩展，不阻塞首个本地可播放版本。

## 开源项目参考

在线音乐平台参考在2026-10-10重新核查，固定版本、指定文件、官方文档与匿名实测分列在[平台能力调查](docs/platform-capabilities.md)和[验证记录](docs/validation/platform-research.md)。下列项目不作为隐式运行依赖；采用Go独立适配时仍需分别核对代码来源和实际能力，账号及完整音频不能由资料查询成功推定。

| 平台 | 开源项目及许可证 | 计划参考内容 | 当前边界 |
| --- | --- | --- | --- |
| 网易云音乐 | [api-enhanced](https://github.com/NeteaseCloudMusicApiEnhanced/api-enhanced/tree/2db6684453297ee86dec0492842f2320b08182d9)，[MIT](https://github.com/NeteaseCloudMusicApiEnhanced/api-enhanced/blob/2db6684453297ee86dec0492842f2320b08182d9/LICENSE) | `v4.41.1` / `2db6684453297ee86dec0492842f2320b08182d9`；搜索、详情、公开推荐及歌单字段 | 只为实际通过的能力宣告支持；不采用unblock或其他来源补解锁分支 |
| QQ 音乐 | [QQMusicApi](https://github.com/jsososo/QQMusicApi/tree/13b08afd3180cc74d76fff208956b77a560abd22)，[GPL-3.0](https://github.com/jsososo/QQMusicApi/blob/13b08afd3180cc74d76fff208956b77a560abd22/LICENSE) | `13b08afd3180cc74d76fff208956b77a560abd22`；历史接口结构、ID和账号范围调查 | 不复制其源码，不引入运行服务；旧接口每项重新验收，账号Cookie不隐式共享 |
| Apple Music | [apple-music-webplayer](https://github.com/zachomedia/apple-music-webplayer/tree/87dbad9984691b927e5c13ca53006372a4257cdd)，[MIT](https://github.com/zachomedia/apple-music-webplayer/blob/87dbad9984691b927e5c13ca53006372a4257cdd/LICENSE) | `87dbad9984691b927e5c13ca53006372a4257cdd`；MusicKit授权与浏览器播放器分工 | 不是Go音频流SDK；实际能力以Apple官方API/MusicKit契约及授权实测为准 |

核查时三个仓库均未归档；QQ与Apple标签查询为空，未将其package版本伪称发布tag。未归档不等于持续维护，一次匿名成功不代表接口稳定。调查与生产验收分开记录，参考项目许可也不等于平台内容或账号权限。

Apple Music 的资料接口与播放分别核对 [Apple Music API](https://developer.apple.com/documentation/applemusicapi) 和 [MusicKit 官方文档](https://developer.apple.com/musickit/)；完整音乐播放按官方播放器和用户授权能力单独适配，不将 catalog / preview 地址等同于可经 Server DSP 的整曲资源。插件分阶段步骤见[插件实现计划](docs/implementation/plugins.md)。

### 本地 NCM 解密参考

NCM 插件已按以下 MIT 项目的容器布局和解密流程，用 Go 标准库重写为独立 Worker，并接入扫描、播放与 Web。它负责本地加密容器，不属于网易云在线 API 插件；没有把参考项目作为运行依赖。配置、手工验证及限制见 [NCM 插件说明](docs/ncm.md)。

| 参考项目 | 许可证 | 参考内容与采用方式 |
| --- | --- | --- |
| [taurusxin/ncmdump-go](https://git.taurusxin.com/taurusxin/ncmdump-go) | [MIT](https://git.taurusxin.com/taurusxin/ncmdump-go/src/branch/main/LICENSE) | 参考 `ncmcrypt/ncmcrypt.go` 的文件头、密钥/元数据区和封面填充布局；重写解析和随机读取，加入严格边界检查，不采用联网补封面或改写音乐标签的逻辑 |
| [iqiziqi/ncmdump.rs](https://github.com/iqiziqi/ncmdump.rs) | [MIT 声明](https://github.com/iqiziqi/ncmdump.rs/blob/master/Cargo.toml) | 对照 `crates/ncmdump/src/ncmdump.rs` 的音频载荷偏移与读取/定位语义；不引入 Rust 运行依赖 |

核心、真实 Worker、Server 端到端和 Edge 三项 NCM 联调已通过；浏览器样本由独立 Node 生成器封装本项目 CC0 MP3/FLAC，另经 Go 核心逐字节校验。已执行 299,534 次模糊测试，Linux 真实 Worker 测试通过。升级依赖后的全量检查、竞争检查和四程序 Windows/Linux 构建均通过，详细证据见[实施记录](docs/validation/progress.md)。后续真实双根扫描发布 1,943 条来源，并通过 6 个真实文件（含 4 个 NCM）的 HTTP 播放、定位及原件不变校验，见[真实库验收](docs/validation/real-local-library.md)；结论不外推到所有网易云客户端下载版本。

歌词/封面的独立上传、预览与人工锁见[内容接口](docs/media.md)；本地内嵌/旁置候选、批量任务、人工选用、导入歌词和自动补缺见[补全任务](docs/metadata.md)。默认关闭的[显式原件写回](docs/writeback.md)只支持逐根授权的本地NTFS FLAC，当前通过API操作。最简Web仍保持列表与播放。
