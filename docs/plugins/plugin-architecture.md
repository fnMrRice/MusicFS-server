# Server 插件架构

Server 保持 Go 模块化单体，官方内置插件运行于 Server 主进程；核心负责权限、状态、事务发布和生命周期。共同边界见[总体架构](../../../docs/architecture.md)与[跨端插件边界](../../../docs/plugin-boundaries.md)。

## 当前能力

CD 专辑、ASMR 和电台在 `plugins/content` 中通过进程内接口分别注册作品库入口、署名与编号。普通音乐由宿主曲库管理。内容插件随 Server 编译，作品库的注册及可选曲目展示契约见[作品资料](../library/works.md)。

默认交付 `musicfs` 与 `musicfs-plugins` 两个程序；运行时只需启动 Server。来源、Scraper、解码、DSP、SQLite 以及分析、标签、评分、内容变换实现均编译到 `musicfs`，在主进程执行。每个实例或任务仍有独立上下文、账号状态和取消范围。`musicfs-plugins --plugin <用途>` 保留为可选的外部进程入口，只有显式选择进程模式才执行。源码分为 Server、SDK、在线插件三个 Go 模块，见[SDK 与构建](plugin-sdk.md)。

| 用途 | 当前职责 | 协议与限制 |
| --- | --- | --- |
| `local` | 本地文件及已挂载 NAS 的观测、元数据、受限资源解析 | 通用 `provider`；根授权、稳定身份、扫描发布由宿主执行 |
| `netease`、`qqmusic`、`applemusic`、`bilibili` | 各平台声明的账号控制及资料能力 | 通用 `provider`；按能力声明判断账号要求，真实账号验收与能力范围见[平台登录](../providers/platform-login.md) |
| `scraper-local` | 音频标签、容器附件及旁置文件中的歌词、封面候选 | 通用 `scraper`；不选用内容、不写原件，正文走任务二进制通道 |
| `audio-ffmpeg` | 探测、解码、编码、规范化及有序 DSP 链 | 通用 `audio`；固定任务和受控输入/输出，见[音频处理](../playback/audio-processing.md) |
| `ncm` | 解密 NCM 外层容器，保留 MP3/FLAC 编码字节并提供独立封面附件 | 通用 `audio`；宿主将附件交 Scraper 验证，源资产与解密表示分离，见[NCM](../playback/ncm.md) |
| `dsp-gain` | 无状态、保持帧数和时标的 PCM 增益 | [DSP v1](dsp-plugins.md) 专用协议；零延迟、无尾音 |
| `storage-sqlite` | 主库与实例状态库的建库、Schema、迁移、原子业务操作与快照 | [持久化专用协议](storage-plugins.md)；默认后端，可由实现同一契约的注册后端替换，运行期间不能停用 |
| `analysis.chromaprint` | 规范化音频的 Chromaprint 指纹提取 | 内置分析接口；仍需校验 `fpcalc` 工具，不新增 BPM 或调性算法 |
| `tags.flac` | FLAC 标签补丁、正文生成及音频范围识别 | `sdk/tagformat`；授权、原件保护、替换和恢复保留在宿主 |
| `matching.standard` | 标签、时长与指纹证据的候选评分 | 内置评分接口；人工排除、最终关联与事务由核心决定 |
| `transform.thumbnail` | PNG/JPEG 封面的缩略图变换 | `sdk/contenttransform`；源内容、版本与最终发布保留在宿主 |

命令行选择值与通用 Manifest 的 `provider/scraper/audio` 三种角色是不同层次。分析、标签、评分和内容变换通过进程内 Go 接口组装，没有新增外部进程入口。DSP 和存储不向通用 Manifest 添加角色，也不使用普通实例启停接口；基础能力在管理列表中只读展示，修改部署配置后重启生效。

插件管理的功能分类同样独立于上述角色和接口：`plugins/transform/thumbnail`、`plugins/tags/*`、`plugins/scraper/*` 统一声明 `plugin.features=["metadata"]`，显示为“元数据”。本地刮削仍保留 `scraper` 角色及歌词、封面的业务能力，标签重写与缩略图仍使用各自的进程内接口；插件 ID 和源码目录保持稳定。

Chromaprint 指纹提取与标准录音匹配统一声明 `plugin.features=["audio-analysis"]`，显示为“音频分析”；两者仍分别使用分析和评分接口，录音匹配的显示名、候选能力与设置页面保持原有职责。

算法管理清单由可信装配通过 `plugincontrol.RegisterBuiltin` 声明身份、发行版本、能力及业务开关；默认构建在 `builtinplugins` 集中注册四种官方算法。替代构建使用 `NewWithBuiltins` 注入实际算法清单，管理器不会补回 Chromaprint 或 FLAC。声明只反映装配与配置，不宣称工具可用或任务正在运行；算法仍通过各自的业务接口执行。

普通插件通过可信装配注册身份、角色、版本、配置和运行工厂；未注册 ID、未知角色和方法仍被拒绝。目录、音频处理和在线平台能力不再由业务层的官方 ID 分支决定。Worker 声明不能授予额外文件、网络或凭据权限。当前没有任意第三方自动发现、下载、安装、升级回滚或管理 CLI。

目录型 Provider 使用自身的导入配置声明；作品包访问要求 `library.packages` 能力，来源 ID 和资源引用为彼此独立的不透明标识。文件替换后的身份重绑定要求 `library.rebind.stable-paths-v1`：插件显式采用 `sdk/directoryidentity` 的状态协议，宿主按同一协议校验变更计划并负责授权、日志发布和恢复。未声明该能力的插件状态不按此格式解释。在线插件提供登录方式、资源 ID/URL 规则和图片域名，SDK 只执行通用结构与策略校验。播放依赖 `audioruntime.Processor/Decoder` 和公共音频类型，解码器按格式链选择；Scraper 使用通用 Host/Session 接口，DSP 节点及进程内执行器通过注册工厂接入。指纹身份由分析插件声明，标签支持格式由 `tagformat.Format` 声明；不同分析身份的证据不能混用。

## 宿主与插件的职责

| 插件可以提供 | Server 必须校验和提交 |
| --- | --- |
| 平台登录交互、账号状态及上游资料 | 用户/实例归属、凭据范围、业务登录门槛及账号撤销 |
| 文件观测、基础标签和资源版本证据 | 授权根、SourceTrack/SourceAsset 身份、扫描完整性及缺失复核 |
| 指纹、候选分数或媒体变换结果 | 输入版本、证据有效性、人工禁止关联、时间轴证据和任务预算 |
| 歌单分页、推荐顺序 | 完整快照、出处、隐藏意图、幂等和事务；失败不发布成功空列表 |
| 受控音频输出 | 来源选择、依赖租约、attempt/表示版本、Range、定位及恢复预算 |
| 歌词/封面候选及正文 | 类型/尺寸/摘要、预览、人工锁、内容发布、原件安全写回 |
| 可信后端的建库、Schema、迁移、原子操作与快照 | 业务数据与事务契约、实例隔离、启动锁、生命周期、文件保护协调和恢复判断 |

`host.state.*` 与 `host.database.*` 只提供实例隔离的受限状态，不提供任意 SQL 或核心数据库权限。所有结果先检查大小、类型、归属、输入版本、运行代次和目标修订，再进入核心发布流程。

## 协议、安全与生命周期

- 已有来源、Scraper、解码与 DSP 内置适配沿用原协议，在独立内存管道和 goroutine 上运行，不创建插件子进程。通用控制面保留 JSON-RPC 2.0、`Content-Length` 帧、UTF-8、1 MiB 消息上限和 `h-` / `w-` 请求 ID；外部进程模式才使用 stdio。精确字段以[协议说明](../contracts/plugins/v1/README.md)与 Schema 为准。
- `host.hello` 校验实例、nonce、配置修订、角色、发行版本及协议；握手前不接业务。`$/cancelRequest` 统一取消，晚结果只能回收，不能发布。
- 音频、NCM 与 Scraper 使用各自的任务限定二进制通道，不把正文放进控制 JSON。网络声明不能替代文件根、平台目标或 NAS 地址授权。
- 管理停用只保存配置，当前 Worker、任务、播放和新工作准入保持，重启 Server 后生效。启用与实例设置可动态应用；设置重建关闭目标实例准入并等待旧租约，详见[插件管理](plugin-management.md)。
- 编译内插件是受信任代码，与 Server 共享地址空间，不提供故障或权限沙箱。外部进程模式的摘要核验和受控 Broker 同样不构成 OS 沙箱。未知 ID 不会映射成主进程内可执行函数。
- 内置协议关闭会取消当前请求、关闭双向管道并等待处理函数以及数据通道内已开始的宿主读写退出，再回收任务槽位。FFmpeg、ffprobe 与 fpcalc 是按需调用的外部工具，仍受各自的探测、摘要、超时和清理约束。
- 内置代码必须通过错误返回处理可预期失败；未恢复的 panic 属于 Server 进程故障，内存通道不提供进程级故障隔离。
- `dspPlugins[].builtin=true` 要求可信装配中存在对应执行工厂，并要求音频处理器支持进程内 DSP。默认装配提供 `musicfs.dsp.gain`；外部音频 Worker 不会自动获得宿主的进程内工厂。内置音频握手还需声明 `audio.dsp.builtin`。

升级内置实现需要升级 Server，先正常停服并保留旧制品及状态快照。显式外部进程模式还须更新所选文件的固定摘要并重新握手；不能覆盖运行中的程序。状态不兼容时拒绝继续启动，不能让旧实现写入新格式。安装与回滚的自动化入口仍未实现。

## 待扩展范围

以下是设计边界，不是可直接加入当前配置的功能。

| 扩展 | 前置条件 |
| --- | --- |
| 原生 NFS/SMB、Bilibili 与在线整曲 | 远端资源模型、目标授权、稳定字节版本和受控读取；见[NAS / Bilibili 计划](../design/nas-bilibili.md) |
| 有状态 DSP、重采样、任意音频图 | 明确延迟、尾音、预滚、格式变化、定位与失败策略；现有 DSP v1 不覆盖 |
| 更多分析、内容变换、标签及策略实现 | 已有四类进程内接口；BPM/调性、更多标签格式、歌词对齐等仍需具体实现和兼容性验证 |
| C++ Worker / SDK | 真实 Go Host 互通、严格 DTO、二进制通道、取消及目标平台制品验证 |
| 第三方安装与更新 | 独立身份注册、制品信任、配置迁移、恢复与额外系统隔离验证 |

新增扩展先判断能否复用既有角色，再同步协议/Schema、宿主注册与校验、Worker、配置迁移和测试夹具。影响公开能力或状态时同步评估 OpenAPI、事件及 Client/Web。使用新旧 Worker 验证接受和拒绝边界后再发布配套制品；协议、插件、算法、配置及资产版本不能互相替代。

需要 C++ 的实现统一使用 [CMake / C++23](../../../docs/native-plugins.md)。显式外部模式可采用独立 Worker；未来编译内 C++ 实现需要经过验证的主进程桥接，目前尚未实现。Client 的原生插件在同一原生宿主进程加载动态库，与 Server 分别适配、构建、发布和验收；可共享算法源码和测试向量，不承诺共用二进制。当前尚无已验证的 C++ Worker 或通用 C++ SDK。
