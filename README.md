# MusicFS-server

MusicFS的Go后端服务。已实现本地扫描/音乐库、鉴权与原流Range、Go DSP与NCM播放、资产摘要/指纹及人工关联、歌词/封面/专辑、受限写回、匿名在线资料、推荐和歌单。独立Web仍为最简列表播放器，新增管理能力通过API使用。启动见[运行说明](docs/running.md)，安装与数据恢复见[部署](docs/deployment.md)、[备份](docs/backup.md)，每步证据及未验范围见[实施记录](docs/validation/progress.md)和[文档核对](docs/validation/documentation-audit.md)。

配置已统一为 `configVersion: 1` 的 YAML，插件与实例显式登记和启用；严格解析、修订写入与崩溃恢复见[配置说明](docs/configuration.md)和[配置验收](docs/validation/configuration.md)。旧 JSON 配置不兼容。开关和配置应用 HTTP API 已实现：停用保存后重启 Server 生效，启用可动态应用，实例设置由 Manager 排空/重建后报告操作结果；管理 CLI、通用安装和第三方 SDK 尚未交付，见[管理验收](docs/validation/plugin-management.md)。

实现范围与后续边界：

- 插件管理：已实现插件与实例双层开关、工作租约、动态启用、配置应用与能力失效通知；停用只保存配置，重启 Server 后生效，见[插件管理](docs/implementation/plugin-management.md)。新增插件仍需 Host 注册/角色适配，不能仅添加 manifest 即使用。
- Music Provider：本地文件与受限在线资料已实现；实体NAS仍需实机验收。当前只有网易公开资料能力实测可用，QQ旧搜索近期失败、Apple目录缺授权。[平台登录增量](docs/platform-login.md)已接入客户端请求及 Web 入口，网易/QQ 二维码创建实测通过；三个在线源统一要求平台登录，网易云业务迁至会话 EAPI、QQ 搜索迁至桌面 RPC，真实用户查询仍待验收；没有在线完整音频。
- 独立本地Scraper已实现内嵌/旁置候选、预览、人工选用和保守自动补缺；查询修正与远程Scraper未实现。内容在独立文件目录，数据库保存引用；默认不改原件，显式写回仅支持逐根授权的Windows本地NTFS FLAC。
- 插件化音频处理：解复用、解码、DSP、编码及封装，向 Web 等客户端交付兼容输出；适用时保留直接播放或代理路径。
- 记录资产码率、完整文件 SHA-512，并以音频指纹辅助匹配；跨端播放与续播行为遵守共同协议。

文档入口：

- [源码导航](src/README.md)：`src/` 按 `app`、`core`、`library`、`metadata`、`playback`、`plugins`、`storage` 七个功能模块组织，列出各模块和子包职责。
- [NFS/SMB NAS Provider](docs/nas-providers.md)、[Bilibili 音频 Provider](docs/bilibili-provider.md)：新增设计，均待实现；B 站匿名优先，收藏夹为主、音频区独立歌单为补充。
- [NAS / Bilibili 增量实施](docs/implementation/nas-bilibili.md)与[开源参考](docs/provider-references.md)：依赖调查、受控远端资源、分阶段交付和验收。

- [Server 分阶段实施](docs/implementation.md)：P0—P8 的文件职责、逐步任务、测试与完成门槛。
- [YAML 配置](docs/configuration.md)：字段、插件与实例、覆盖顺序、配置读写及应用边界。
- [扫描与文件通知](docs/scanning.md)：选根、递归深度、排除规则、默认关闭的监听与周期补漏，以及暂停任务的处理方式。
- [插件实施计划](docs/implementation/plugins.md)：协议宿主、Local Files、音频链、独立 Scraper 和平台适配。
- [插件化细化设计](docs/plugin-architecture.md)：可替换职责、核心权限/数据/事务边界、当前接入缺口与增量验收。需要 C++ 的 Server 插件以 CMake / C++23 构建独立 Worker；Client 的 DLL 与 Server 分别构建，现有 Go 插件继续使用。
- [联合实施总览](../docs/implementation.md)与[最小 Web 实施](../web/docs/implementation.md)：共同接口和真实浏览器播放验收。
- [Server 内部架构](docs/architecture.md)：模块、数据模型、Provider/Scraper、存储与任务管理。
- [Server 音频处理](docs/audio-processing.md)：处理链、播放交付、定位、缓存与资源约束。
- [完整处理输出缓存](docs/processed-cache.md)：完整发布、私有复用、共享配额、引用回收和重启校验，仍使用timeRestart。
- [本地音频指纹](docs/fingerprinting.md)：固定工具、后台任务、独立明细文件、恢复和管理接口；默认禁用，不上传外部服务。
- [候选与人工关联](docs/matching.md)：有界比较、排除优先、修订冲突、解除/拆分和可逆审计；保留全部来源。
- [歌词与封面文件](docs/media.md)：有界上传、私有预览、按来源资产选用、人工锁和版本失效。
- [媒体恢复与备份](docs/media-storage.md)：持久发布阶段、引用保护回收和一致性数据库/媒体快照。
- [来源专辑与发行](docs/albums.md)：显式有序成员、独立封面、人工锁、修订及按专辑刮削。
- [派生媒体](docs/derived-media.md)：封面缩略图、完整刮削记录、授权读取与历史备份。
- [公开推荐](docs/recommendations.md)：按本人在线来源聚合、有限缓存、失败保留与刷新；读取不导入或播放。
- [远程歌单](docs/remote-playlists.md)：显式导入、保守同步、固定本地快照、待复核缺失与取消；保留重复出现。
- [个人歌单](docs/playlists.md)：有序组合、本地追加、完整出处、独立隐藏修订和关联扩大确认。
- [播放恢复](docs/recovery.md)：有限恢复预算、人工时间对齐证据及默认重新播放提示。
- [安装与部署](docs/deployment.md)：默认双程序制品、可选分拆、外部工具、TLS及尚未运行验收的容器/代理示例。
- [完整备份与恢复](docs/backup.md)：停服创建、清单校验、新目录恢复与同平台回滚边界。
- [本地生命周期](docs/lifecycle.md)：独立启动或鉴权租约、多个客户端与有界退出；不包含桌面Client实现。
- [内部组件图](docs/diagrams/components.puml)：可编辑的 PlantUML 源码。
- [跨端架构](../docs/architecture.md)：Server 与客户端共同遵守的协议和行为。
- [工作约定](AGENTS.md)：文档维护与仓库协作规则。

本仓库与 Client、Web 分别管理 Git 历史。跨仓库相对链接按 MusicFS 根目录下 `server`、`client`、`web` 并列检出的工作区布局解析。

工程分为根 Server、`sdk`、`plugins/source/online` 三个 Go module。宿主通过 SDK 使用在线插件，平台请求和登录实现由在线模块维护；模块独立测试，见 [SDK 与模块边界](docs/plugin-sdk.md)。`scripts/build.ps1` / `scripts/build.sh` 默认只构建 `musicfs` 和 `musicfs-plugins` 两个程序。`plugins/unified` 将四种本地角色、三个在线平台、SQLite 和 DSP gain 装配到统一插件入口；每个角色仍独立启动进程。需要单独入口时显式使用 `-SplitPlugins` / `--split-plugins`，额外生成原有六个分拆程序，源码目录和 SDK 边界保持独立。正式安装包要求 SQLite 插件角色，额外 DSP 默认关闭。详见[插件发现与兼容](docs/configuration.md#内置插件程序与自动发现)及[部署说明](docs/deployment.md)；使用 `scripts/check.ps1` / `scripts/check.sh` 检查所有模块。真实音频工具测试需设置 `MUSICFS_FFMPEG` 和 `MUSICFS_FFPROBE`；指纹测试另需 `MUSICFS_FPCALC`，缺少工具时明确跳过对应部分。

第一条交付链为Local Files扫描→音乐库→授权原流→Web实际解码，随后完成解码→PCM DSP→编码/封装→Web播放与时间定位。浏览器非零PCM、频率标记与控制操作已有证据，人工扬声器听音仍待用户验收。P5—P7扩展不改变未匹配来源可播放的规则；所有未测环境单独保留。

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

核心、真实 Worker、Server 端到端和 Edge 三项 NCM 联调已通过；浏览器样本由独立 Node 生成器封装本项目 CC0 MP3/FLAC，另经 Go 核心逐字节校验。首次NCM阶段完成299,534次模糊测试、Linux真实Worker及当时四程序双平台构建；这些是历史验证范围，当前默认构建为上述两个程序（可选分拆为八个），详细证据见[实施记录](docs/validation/progress.md)。后续真实双根扫描发布1,943条来源，并通过6个真实文件（含4个NCM）的HTTP播放、定位及原件不变校验，见[真实库验收](docs/validation/real-local-library.md)；结论不外推到所有网易云客户端下载版本。

歌词/封面的独立上传、预览与人工锁见[内容接口](docs/media.md)；本地内嵌/旁置候选、批量任务、人工选用、导入歌词和自动补缺见[补全任务](docs/metadata.md)。默认关闭的[显式原件写回](docs/writeback.md)只支持逐根授权的本地NTFS FLAC，当前通过API操作。最简Web仍保持列表与播放。
