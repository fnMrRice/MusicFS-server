# Server 架构

跨端职责、身份和播放行为以[总体架构](../../docs/architecture.md)为准；精确接口以本仓库 [OpenAPI 与 Schema](contracts/README.md)为准。使用与专题入口见[文档导航](README.md)，未完成验证见[待验收清单](validation/README.md)。

## 模块与交付

Server 是 Go 模块化单体，统一管理来源、曲库、歌单、资料补全和音频交付。宿主按七类职责组织，子包说明见[源码导航](../src/README.md)。

| 模块 | 职责 |
| --- | --- |
| src/app | 程序入口、服务组装、HTTP 鉴权与请求转换 |
| src/core | 身份、配置、共享类型与生命周期 |
| src/library | 来源与资产、扫描、查询、专辑、歌单、推荐与个人数据 |
| src/metadata | 摘要/指纹、人工关联、独立 Scraper 调度与受限写回 |
| src/playback | 来源/输出选择、会话、音频处理、流、定位恢复与缓存 |
| src/plugins | 内置插件宿主、可选进程通信、协议、实例配置、凭据和受控资源 |
| src/storage | Backend 契约、后端选择与通信、媒体文件及备份恢复 |

目录按功能聚合，不构成严格分层；扫描、识别、补全和写回仍由各业务调度。具体插件在根 plugins/，公共 SDK 和在线平台实现分别是独立 Go 模块，见[开发说明](development/README.md)。

默认运行只启动 `musicfs`，所有官方内置插件编译到 Server 主进程：Local Files、FFmpeg、NCM、本地 Scraper、网易云、QQ 音乐、Apple Music、SQLite、DSP gain，以及指纹、标签、评分和缩略图实现。安装包中的 `musicfs-plugins` 保留九种兼容命令入口，只有显式外部进程配置才执行；它不再是默认运行前提。SQLite 与 DSP 使用专用接口和部署配置，不参加普通插件自动发现。FFmpeg、ffprobe、fpcalc 仍可作为受控外部工具运行。分拆构建与安装配置见[部署](operations/deployment.md)。

当前具备本地/已挂载来源扫描、原流/处理/NCM 播放、识别与人工关联、内容补全与受限写回、在线资料与歌单接口、缓存、备份和生命周期协议。三平台账号接口已接入，真实账号完整登录尚待验收；在线整曲交付尚未实现。原生 NFS/SMB、Bilibili 和通用第三方插件接入仍为设计。

## 身份、权限与版本

| 对象 | 语义 |
| --- | --- |
| Recording | 一个录音版本；Live、Remix、翻唱分别建模 |
| SourceTrack | providerInstanceId + externalId 唯一；未匹配也能播放 |
| SourceAsset | 某来源、音质的不可变字节版本；内容变化创建新资产 |
| RecordingSourceLink / MatchExclusion | 人工关联、排除、证据与修订；关联保留全部来源 |
| MediaObject / MediaBinding | 不可变内容文件，以及资产或来源专辑上的选择与人工锁 |
| PlaylistSnapshot / EntryProvenance | 固定修订条目及全部出处；展示行不是持久身份 |
| PlaybackSession / PlaybackAttempt | 用户会话及固定输入、输出表示和代次的一次尝试 |
| TimelineEvidence | 绑定双方资产版本、适用区间和处理条件的独立时间映射 |

歌名、歌手、ISRC、时长只作证据，不作唯一身份。码率与摘要属于 SourceAsset，未知值为 null。fileSha512 只表示 Server 对稳定版本完整文件字节的 SHA-512；Provider 申报值另存，标签变化也会改变整文件摘要。后台摘要不阻塞入库或播放。

指纹在用户 DSP 之前提取，明细存文件；相似度只支持待确认候选，不自动合并录音或生成时间轴证据。当前本地提取使用固定工具与参数，见[资产识别](metadata/identification.md)；关联、排除及拆分见[匹配](metadata/matching.md)。

来源、账号、私人文件、歌单和会话按 MusicFS 用户授权。登录型插件每实例属于一个用户并承载一个平台账号；管理员扩大管理视图不扩大业务权限。现有 MusicFS 登录仅建立管理员会话，不代表一般用户注册已交付。

## 插件与配置

宿主保留身份、权限、状态所有权、调度与事务发布；Provider 负责来源发现、基础元数据和资源解析，Scraper 独立提供歌词/封面候选，音频插件处理任务授权的输入。它们不能隐式共用凭据，不能直接改宿主核心表。

既有来源、解码和 Scraper 内置适配在每实例或任务独立的内存流上运行版本化 JSON-RPC，DSP gain 保留内存流上的 PCM 帧协议，SQLite 直接使用后端接口。音频、NCM 和 Scraper 正文仍走任务限定的私有二进制通道。普通插件只有显式 `execution: process` 才通过 stdio 启动外部程序；内置代码与 Server 共享地址空间，取消和退出依靠协作，不提供崩溃、内存破坏或卡死隔离。外部进程也不等于操作系统沙箱。当前按已支持身份与角色接入，任意 manifest 不能自动安装为新插件，见[插件架构](plugins/plugin-architecture.md)。

| 新增内置插件 | 接口 | 当前实现与宿主边界 |
| --- | --- | --- |
| `plugins/analysis/chromaprint` | `fingerprintworker.Extractor` | 现有 Chromaprint 指纹；授权读取、调度、资产绑定与结果保存由宿主执行 |
| `plugins/tags/flac` | `sdk/tagformat.Format` | FLAC 标签重写；字段授权、原件保护、摘要校验、替换与恢复由宿主执行 |
| `plugins/matching/standard` | `candidates.Scorer` | 标签、时长、摘要及指纹的标准候选评分；人工排除、确认与录音关联由宿主执行 |
| `plugins/transform/thumbnail` | `sdk/contenttransform.Thumbnailer` | PNG/JPEG 缩略图；对象权限、缓存、父对象版本与发布由宿主执行 |

四个插件各自声明发行版本，直接通过 Go 接口注入，不增加插件进程或外部安装入口。BPM、调性、ID3/MP4、歌词对齐及任意第三方评分加载仍未实现。内置实现版本随 Server 重新构建和交付，插件发行版本与协议、算法、配置版本分别管理。多用户、账号和 UUID 实例边界不因共用进程而合并。

普通插件停用只保存期望配置，当前 Worker、任务、播放及新工作准入保持，重启 Server 后生效。启用和实例设置可动态应用；restartRequired、effectiveEnabled、appliedRevision 区分保存意图与实际运行。旧 drain/force 不再触发热停用，见[插件管理](plugins/plugin-management.md)。

YAML 保存部署、权限和普通插件开关；Local Files 的实际目录及扫描/监听设置由管理 API 保存到宿主管理的插件配置数据库。permissions.readDirectories 是授权范围，添加目录不扩大权限，保存不自动扫描；默认授权及迁移见[实例配置](plugins/plugin-configuration.md)。插件声明角色、名称、版本和多实例能力，实例 UUID、插件 ID 和管理员别名彼此分离。

SQLite 是必需后端，默认在主进程通过直接后端接口运行，显式进程配置才使用独立 Worker。主库的 Schema、内嵌 SQL 和迁移加载器位于 `plugins/database/sqlite/migrations`，实例状态库的建表、迁移及只读读取位于 `plugins/database/sqlite/state`；插件完成初始化后才返回可用后端。宿主保留业务数据与事务契约、后端选择、数据锁和生命周期协调，配置预加载只读且不建库；Server 仓库继续维护契约及插件实现。DSP 支持有序无状态节点，首版要求固定格式/帧数、零延迟和无尾音。数据库与 DSP 在管理列表只读展示，变更部署配置后重启，见[存储插件](plugins/storage-plugins.md)与[DSP 插件](plugins/dsp-plugins.md)。

## 来源与歌单

Local Files 访问 Server 本机或已挂载文件系统；远程客户端的路径不会自动变为 Server 来源。文件打开从授权根受控进行，拒绝越界及链接逃逸。稳定身份不使用路径或摘要替代；可靠重命名保留 SourceTrack，歧义不强行认作移动。

扫描先验证根/卷身份，完整发布新修订；离线、取消、不完整或身份变化保留旧库，不将空挂载当作删库依据。监听仅作提示，周期复核补漏，范围、重试和资源限制见[扫描](library/scanning.md)。原生 NAS 直连另见[设计](design/nas-providers.md)。

在线业务能力与平台授权分别检查；当前音乐平台请求要求已校验账号，资料、推荐、歌单和整曲播放分别声明及验收。Provider 不决定全局匹配或去重，资源不向客户端泄露凭据，见[在线来源](providers/online.md)。[Bilibili](design/bilibili-provider.md)的匿名优先规则仅属于该未实现设计。

远程歌单先完整读取再发布固定修订；没有可靠远端快照版本时，缺失先标疑似移除，后续完整同步或独立查询复核后才移除贡献。失败保留旧内容。聚合按绑定及原顺序合并，已确认录音去重、未知来源按 SourceTrack 去重，保留全部出处；隐藏规则绑定持久身份，不写回平台，见[歌单](library/playlists.md)。

## 内容、写回与持久化

歌词、封面、缩略图、刮削文档和指纹明细存独立文件；数据库保存索引、相对路径、摘要、绑定和任务状态。客户端只获得受鉴权引用。更新先写入并校验不可变文件，再事务切换引用；阶段记录用于恢复，回收保护引用和活动任务，见[媒体存储](metadata/media.md)。

刮削预览不发布，人工选用校验修订；默认关闭的自动补缺不能覆盖人工锁。上传和编辑不依赖在线插件，不自动开启原件写回，见[资料补全](metadata/metadata.md)。

原件写回默认关闭，当前仅支持逐根授权的 Windows 本地 NTFS FLAC。核心协调读写、备份与恢复，成功生成新资产和摘要并保留曲目身份；NAS、Linux 或其他格式不能据通用设计宣称支持，见[写回](metadata/writeback.md)。

SQLite WAL 放本地文件系统；备份同时保存数据库及被引用的媒体和恢复状态，缓存可重建。凭据由宿主按实例保护，配置、日志和客户端响应不得暴露；见[配置](operations/configuration.md)与[备份](operations/backup.md)。

## 播放与生命周期

每次播放固定来源资产与输出表示。原流或 NCM 解密表示使用自身长度和验证器；处理输出不继承输入 Range。timeRestart 按曲目时间生成新尝试，客户端使用返回地址和起点映射；处理链进入表示、缓存与恢复身份，见[音频处理](playback/audio-processing.md)。

恢复限制次数、目标来源和总时长。跨源继续须同时满足同一 Recording、双方资产上的有效独立时间映射及可定位的目标输出；否则提示从头播放，不凭指纹猜偏移，见[播放恢复](playback/recovery.md)。

独立 Server 不因客户端退出而结束；client-managed 仅本机启用并以鉴权租约维持，最后租约结束或首次认领超时后有界收尾。升级先停机、备份和迁移，不覆盖运行程序，见[生命周期](operations/lifecycle.md)。

[组件图](diagrams/components.puml)保留可编辑 PlantUML 源码；新角色、资源通道和部署环境的未完成工作统一见[待验收清单](validation/README.md)。
