# MusicFS Server 音频处理设计

状态：设计草案；2026-10-09。本文补充[Server 架构](architecture.md)，描述内部音频处理。跨端会话、播放描述、恢复与定位契约以[总体架构](../../docs/architecture.md)为准；具体库、插件装载方式和输出格式矩阵待验证。

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

当前基线使用独立官方音频 Worker，内部通过注册表连接 Go PCM DSP 与 FFmpeg 解码/编码，实际进程边界见[插件验证](validation/plugins.md)。未来进程内外部接口如采用，只用版本化 C 兼容数据、不透明句柄和明确内存归属，不跨 C++ ABI，不传运行时对象/异常。第三方原生插件须另验证权限与故障隔离，清单不是沙箱。

构建/配置/换图在控制线程完成，于明确块边界切换，旧图停止使用后释放插件/缓冲。取消上下游传播，失败不能继续发布。必要 DSP 失败明确报错，只有显式配置可旁路的效果才允许旁路并提示。

## 3. 协商、表示与缓存

终端提交 codec、container、MIME、传输和 seek 能力，Server 据插件、权限和配置选已验证 profile，含采样率、声道、目标码率及处理链版本。AAC/fMP4、Opus/WebM 仅为候选；浏览器/MSE 按 MIME/codec 探测并实际验证，没有共同路径就返回可解释错误，不无限更换编码器。

效果明确在 Server 或 Client 执行并报告实际链，避免 EQ/增益重复处理；桌面换曲淡出淡入归 Client，Server 不写入桌面转场。

PlaybackAttempt 固定 inputAssetId、来源、插件/参数版本、profile、起点和 representationId。来源、字节版本、处理参数、编码配置或起点改变时生成新表示及 attempt URL；新 attempt 必须使用新 URL，不能沿用旧 Range/缓冲。仅授权刷新且源字节、输出表示完全未变时可按总体契约保留 attempt，验证原偏移后继续。

缓存键至少含源版本、访问归属、处理链/参数、插件版本、profile、起点/范围；不得跨账号/用户复用未授权来源，命中仍查当前权限。临时和转码输出存独立 cache 文件，DB 只存索引/状态。活动 attempt 持有引用，不能回收仍使用对象；中断产物不标完整，校验后发布，临时/孤儿过宽限期清理。

## 4. Range 与时间 seek

原字节代理流式转发，支持背压、取消、单 Range、准确 Content-Type 和真实 206/416，不在内存缓冲全文件；上游无 Range 不伪造 206。当前本地基线先建立有磁盘配额的私有固定副本，以保持后续请求的字节一致性；处理输出边生成边发送，不持久缓存。转码输出不能继承原文件长度、ETag 或偏移。

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

音频插件禁用遵循[插件管理](implementation/plugin-management.md)：默认关闭新播放/新任务准入，活动播放保留固定输入、处理配置与有限租约直到结束；现有租约内的 Range、同资产定位可继续，但不能借恢复或切歌扩大到新工作。管理员强制停止时取消相关管线、撤销流并回收进程。禁用必需 DSP/编码插件时明确返回不可用，不静默移除效果；与该插件无依赖的原流仍可播放。

输入版本变化使未完成产物/映射不能用于当前版本，播放进入失败或重解析，不拼接新文件尾部；写回遇到活动播放按架构延后。崩溃后据持久阶段清理或重建，“进程曾启动”不代表产物完整。停机先拒绝新任务，再取消/收尾并有界退出，插件失败不得破坏歌单、Media Store 或音乐原件。

## 7. 验证与依据

验证完整管线到实际浏览器的首帧、连续播放、暂停、时间定位及三种路径选择；实际测量格式、音质、资源和延迟，不从库文档推定全平台支持。

验证源版本/账号/参数/插件变化不误复用缓存，无 Range 不伪造 206，实时输出不接受原文件偏移，新 attempt 有新 URL，晚结果不覆盖当前；验证 DSP 预滚/延迟、前导静音差异和无证据不续播。

验证慢消费者背压、断连取消、插件超时/崩溃、回收和停机；必要 DSP 失败可见，两端不重复处理，私密请求头不暴露，缓存/会话不越权。

- [FFmpeg：解复用、解码、过滤、编码与封装](https://ffmpeg.org/ffmpeg.html#Detailed-description)
- [W3C：Media Source Extensions](https://www.w3.org/TR/media-source-2/)

资料界定底层能力，不替代项目验证。Client 的 WMF/FFmpeg、输出时钟、主题、淡化和控制插件在 Client 文档维护。
