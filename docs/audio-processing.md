# MusicFS Server 音频处理设计

状态：长期设计与实现约束；2026-10-20。本文补充[Server 架构](architecture.md)，描述内部音频处理。独立音频 Worker、版本化有序 DSP 链、[独立进程 DSP v1](dsp-plugins.md)、MP3 输出及 [NCM 解密](ncm.md)已有实现和测试，具体平台与格式边界见[实施记录](validation/progress.md)及 DSP 文档的验证记录，其余设计能力仍待实现或验证。跨端会话、播放描述、恢复与定位契约以[总体架构](../../docs/architecture.md)为准；插件职责与 C++ 接入步骤见[插件化细化设计](plugin-architecture.md)。

## 1. 角色与路径

Provider 给授权来源与资源描述，Audio Processor 做媒体处理，Resolver 选择来源/profile，Stream Gateway 鉴权交付；Scraper 不承担音频流传输。一个或多个插件可提供 audio.demux、audio.decode、audio.dsp、audio.encode、audio.mux，经 Server Host 独立注册。

```text
Authorized source
  -> Demux -> Decode -> PCM -> DSP chain
  -> Encode -> Mux / Streaming package
  -> Stream Gateway -> Web / capable client
```

无 DSP 且格式兼容时直放或字节代理；仅容器不兼容且可安全转换时重新封装；需要 DSP/转码才进完整链。DSP 可为空，编码包不能交给 PCM DSP，编码器裸包不一定能由浏览器播放。输出只用于播放/缓存，不修改原音乐。

本地/NAS、私密请求头或终端不可达来源经 Gateway；直放仅给终端可访问且不泄露凭据的 URL。转码不绕过来源授权、平台限制或未验证能力。

## 2. SDK 与数据契约

manifest 包含运行端/平台、版本、能力、输入输出格式、参数 schema、延迟、seek 和权限。Server 与 Client SDK 分别版本化，模型可共享但不承诺二进制通用；通用基础设施见[插件宿主](architecture.md#3-插件宿主与-sdk)。

PCM 块定义采样格式/率、声道数/布局、帧数、时间戳、时间基准和缓冲所有权。重采样/声道转换为显式节点；DSP 申报延迟、尾音、预滚及时间映射，处理图保存曲目时间到输出时间的关系。

控制面覆盖能力、构建/配置、开始、取消、seek/reset、flush/drain、错误和释放。数据面为有界二进制队列/受控共享缓冲，不用 Provider/Scraper RPC 传 JSON/Base64 PCM。Host 负责背压、上限、超时、配额、断连回收和故障隔离。

当前基线使用独立官方音频 Worker，内部通过注册表连接 Go PCM DSP 与 FFmpeg 解码/编码；`audioDSP.chain` 按顺序执行内建节点或配置的外部可执行节点。外部节点使用专用 DSP v1 stdin/stdout 帧，PCM 为有界二进制，启动前校验可执行文件 SHA-256；实际进程边界见[DSP 插件](dsp-plugins.md)和[插件验证](validation/plugins.md)。需要 C++ 的音频插件可按该协议构建独立 Worker，当前提供 Go SDK 与可构建的外部 gain 示例，尚无已验证的 C++ DSP 实现。共享算法可另适配 Client 的单进程 DLL，Server 不直接加载 Client DLL。Worker 内未来如需可替换动态节点，应使用版本化 C ABI、不透明句柄和明确内存归属，不跨 C++ ABI，不传运行时对象/异常。第三方原生插件须另验证权限与故障隔离，清单不是沙箱。

当前 DSP 部署配置重启生效，既有会话保留创建时的处理器与链；运行时热换图尚未实现。未来换图须在控制线程完成，于明确块边界切换，旧图停止使用后释放插件/缓冲。取消上下游传播，失败不能继续发布。当前所有配置节点均为必需节点，失败明确报错，无自动旁路配置。

### 2.1 当前协议与目标节点接口的区别

当前 [v1 Schema](contracts/plugins/v1/protocol.schema.json)的音频模型固定 `f32le / 48000 Hz / stereo`、`mp3-128` 输出，以及包含 `profile/gainDB/positionMs` 的计划。`audio.prepare/start` 新增可选 `dspChain/dspPlugins`，先执行用户 gain，再执行有序配置链，版本固定为 1；外部插件声明且必须遵守无状态、保持帧数/时标、零延迟、无尾音。`audio.normalize` 是独立的指纹规范化路径，跳过整条播放 DSP 链。`audio.describe/prepare/start`、NCM 专用方法和统一取消通知已实现；外部 DSP 专用协议有 reset/drain，但音频 JSON-RPC 不接受任意图、`seek/reset/flush/drain` 方法或 DLL 路径。

官方 FFmpeg Worker 的实现版本随该扩展升为 `0.2.0`，Host 精确校验版本后才发送音频请求。旧 `0.1.0` 即使忽略未知字段也不能被当作已执行 DSP；包括空链部署在内，Server 与音频 Worker 均须配套升级。

下一步目标为更通用的“Host 选择和验证图、Worker 执行媒体算法”：解密、解复用、解码、DSP、编码和封装各自声明受支持格式、参数修订、状态重建、预滚、延迟和尾音；按需组合到一个或多个 Worker，不要求每个 PCM 节点都跨进程。当前外部扩展边界是 DSP v1 帧协议；重采样、变速、有状态滤波、预滚、尾音和 Worker 内动态 C ABI 仍需另行设计与验证，不能把 Go 内部 `Node` 接口当作第三方二进制 ABI。

| 节点边界 | 插件责任 | Host 责任和验收 |
| --- | --- | --- |
| 字节 → 容器/编码包 | 容器校验、解密/解复用、轨道和格式描述 | 只授指定资产，拒绝格式伪报、越界及输入版本变化 |
| 编码包 → PCM | 解码、实际采样格式、声道、时间戳 | 格式协商、长度校验、背压；未支持声道/格式明确失败 |
| PCM → PCM | DSP/重采样、延迟/尾音、可重建状态和映射 | 固定链顺序、参数版本、缓冲上限及旁路策略；两端不重复执行 |
| PCM → 编码/封装 | 编码、封装、结束排空和错误报告 | 终端兼容、真实 seek、完整性验证和受权交付 |
| DSP 前分析分支 | 指纹规范化/算法、范围和版本 | 只采用户 DSP 前输入；保存证据，不直接关联 Recording 或允许跨源续播 |

图或算法变化需要更新处理链/插件/参数版本并生成新表示；旧图与正在播放的 attempt 绑定到原租约，排空后才释放。跨端只共享格式和时间语义，Client DLL 的加载、实时回调和崩溃范围由 Client 设计负责。

## 3. 协商、表示与缓存

终端提交 codec、container、MIME、传输和 seek 能力，Server 据插件、权限和配置选已验证 profile，含采样率、声道、目标码率及处理链版本。AAC/fMP4、Opus/WebM 仅为候选；浏览器/MSE 按 MIME/codec 探测并实际验证，没有共同路径就返回可解释错误，不无限更换编码器。

效果明确在 Server 或 Client 执行并报告实际链，避免 EQ/增益重复处理；桌面换曲淡出淡入归 Client，Server 不写入桌面转场。

配置非空 DSP 链时，即使来源可原样播放也选择 `processed`，要求终端支持当前 `audio/mpeg` 输出。`effectiveProcessing` 依次报告用户 gain 和各配置节点的名称、版本、参数及 `execution: server`。空配置仍使用 `gain-v1`；配置链修订为 `dsp-v1:<SHA256>`，绑定有序节点、参数、外部可执行配置与摘要、音频 Worker 摘要和实现版本，并用于处理缓存与恢复证据。固定时标的 v1 节点允许按起点重建，但不因此证明不同音源的时间轴等价。

PlaybackAttempt 固定 inputAssetId、来源、插件/参数版本、profile、起点和 representationId。来源、字节版本、处理参数、编码配置或起点改变时生成新表示及 attempt URL；新 attempt 必须使用新 URL，不能沿用旧 Range/缓冲。仅授权刷新且源字节、输出表示完全未变时可按总体契约保留 attempt，验证原偏移后继续。

缓存键至少含源版本、访问归属、处理链/参数、插件版本、profile、起点/范围；不得跨账号/用户复用未授权来源，命中仍查当前权限。临时和转码输出存独立 cache 文件，DB 只存索引/状态。活动 attempt 持有引用，不能回收仍使用对象；中断产物不标完整，校验后发布，临时/孤儿过宽限期清理。

## 4. Range 与时间 seek

原字节代理流式转发，支持背压、取消、单 Range、准确 Content-Type 和真实 206/416，不在内存缓冲全文件；上游无 Range 不伪造 206。当前本地基线先建立有磁盘配额的私有固定副本，以保持后续请求的字节一致性；处理输出边生成边发送，完整成功后可发布[有界处理缓存](processed-cache.md)。命中仍复核授权及当前资产，保持 `processed/timeRestart`，不提供原流 Range，也不继承原文件长度、ETag 或偏移。

播放描述遵守公共 seekMode、有效范围和 positionOriginMs 契约。只有稳定、可验证且可随机访问的输出字节表示提供 byteRange。实时处理通常 timeRestart：按曲目时间定位输入、预滚解码、重建/reset DSP 与编码器，封装新表示；终端加载新的 attempt URL 并映射回曲目时间。DSP 状态不能重建时不声明对应区间可定位。

创建使用 `POST /playback/sessions`，恢复使用 `POST /playback/sessions/{id}/recover`，定位使用 `POST /playback/sessions/{id}/seek`。字段/并发规则遵守总体契约并由 OpenAPI 落实；URL 从 attempt 描述取得，不复用一个内容可变的固定会话 stream URL。

seek/recover 以会话代次和幂等保护协调；新 attempt 生效后取消无消费者旧任务，晚结果不能覆盖当前。定位失败返回真实状态，不假报恢复成功。同 SourceTrack 仅 profile/DSP 重建可依据经过验证的曲目到新输出映射定位，不要求先关联 Recording；跨来源恢复另满足下一节条件。

## 5. 恢复与时间轴

终端报告 attemptId、可观察错误及曲目时间，Server 决定有限同源刷新或换源，不要求浏览器识别上游签名过期。同时限制同源刷新次数、不同来源总数（最多 3 个）及总时间预算，同 attempt 并发 Range 失败合并处理，禁止无限重试。

跨源自动续播必须确认同一 Recording、新链可定位，且目标处于已验证的跨资产时间映射区间。指纹相似、相同时长、Recording 相同或单纯可 seek 都不足。TimelineEvidence 绑定两端资产版本、映射、区间、方法/版本，覆盖前导静音、裁切和编码延迟；Pipeline 再合成 DSP/封装时间映射，避免变速、重采样和延迟混淆曲目时间。

证据失效或定位失败则提示由用户决定从头播放，不猜位置。符合条件的新 attempt 允许短中断，终端丢旧缓冲后加载。文件字节一致与时间轴兼容分开判断；同源刷新只有可靠验证器确认输入和输出未变才允许保持原偏移。

歌词来自 Media Store，优先匹配当前来源/版本；切源后校验时序和处理后映射。用户偏移单独保存，不修改原歌词，也不把偏移当跨源验证证据。

## 6. 任务与资源治理

任务记录 ID、session/attempt、取消令牌、输入/输出版本、阶段/状态及错误。播放优先于后台摘要/指纹；指纹取用户 DSP 前的音频，不能把用户处理输出冒充原资产指纹。

按实例/用户限制并发、CPU、内存、缓冲、缓存、空闲和总生命周期；有界排队并向上游传播背压，慢终端不能无限积压。取消/断连回收无消费者任务及临时输出，重连宽限期有界；必要处理失败不得静默移除。

音频插件停用遵循[插件管理](implementation/plugin-management.md)：只保存开关，当前管线、Range、定位和新播放/任务继续按当前运行能力工作，重启 Server 后才停用。旧 force 参数不取消管线或回收 Worker。实例配置重建仍按工作租约排空；正常 Server 退出仍回收相关资源。重启后必需 DSP/编码插件不可用时明确报错，不静默移除效果；无此依赖的原流仍可播放。

输入版本变化使未完成产物/映射不能用于当前版本，播放进入失败或重解析，不拼接新文件尾部；写回遇到活动播放按架构延后。崩溃后据持久阶段清理或重建，“进程曾启动”不代表产物完整。停机先拒绝新任务，再取消/收尾并有界退出，插件失败不得破坏歌单、Media Store 或音乐原件。

## 7. 验证与依据

验证完整管线到实际浏览器的首帧、连续播放、暂停、时间定位及三种路径选择；实际测量格式、音质、资源和延迟，不从库文档推定全平台支持。

验证源版本/账号/参数/插件变化不误复用缓存，无 Range 不伪造 206，实时输出不接受原文件偏移，新 attempt 有新 URL，晚结果不覆盖当前；验证 DSP 预滚/延迟、前导静音差异和无证据不续播。

验证慢消费者背压、断连取消、插件超时/崩溃、回收和停机；必要 DSP 失败可见，两端不重复处理，私密请求头不暴露，缓存/会话不越权。

- [FFmpeg：解复用、解码、过滤、编码与封装](https://ffmpeg.org/ffmpeg.html#Detailed-description)
- [W3C：Media Source Extensions](https://www.w3.org/TR/media-source-2/)

资料界定底层能力，不替代项目验证。Client 的 WMF/FFmpeg、输出时钟、主题、淡化和控制插件在 Client 文档维护。
