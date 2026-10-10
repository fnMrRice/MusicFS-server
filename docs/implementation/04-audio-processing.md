# P4：插件音频处理和 Web 时间定位

状态：解码→Go PCM 增益→MP3 封装、timeRestart 和资源回收已落地；具体实测与待验门槛见[实施记录](../validation/progress.md)和[插件验证](../validation/plugins.md)。返回[实施入口](../implementation.md)。只声明已有证据的输出组合。

## 1. 目标链路与文件职责

```text
SourceAccess authorized input
  -> Demux / Decoder adapter
  -> bounded PCM blocks
  -> Go gain DSP node (initial proof)
  -> Encoder / Mux adapter
  -> attempt output stream
  -> Web audio
```

| 生产位置 | 主要工作 | 根 tests |
| --- | --- | --- |
| src/audio | profile、处理计划、PCM 契约、图生命周期、时间映射 | unit/audio、integration/playback |
| src/audio/dsp | 首个增益节点及参数/重置/延迟报告 | unit/audio、contract/plugins |
| src/plugins/audioffmpeg | FFmpeg 构建探测、decoder/encoder/mux 适配 | integration/plugins、integration/playback |
| src/app/musicfs-plugin-audio-ffmpeg | 薄 worker 入口 | testapps 替身、integration/plugins |
| src/sourceaccess | 任务限定 input broker 与版本约束 | integration/playback |
| src/playback、src/stream | processed 尝试、定位、新输出和消费者取消 | contract/api、integration/playback |

## 2. P4A：先验证封装和真实流输出

### S-P4-01：工具可用性与能力清单

1. 固定 ffprobe/FFmpeg 可执行文件来源、版本、构建选项、平台和分发许可记录；工具路径只能来自受控配置。
2. 读取实际可用的 demuxer/decoder/encoder/muxer 能力，形成 Host 能力报告；缺组件返回 UNSUPPORTED，不默默选另一个音质配置。
3. 实例启动探测仅提供候选，真实打开/输出验证决定是否发布 profile；不给所有安装 FFmpeg 的机器相同能力声明。
4. 覆盖可执行文件缺失、错误版本、输出异常、挂起和超长 stderr，错误不得导致无界进程派生。

### S-P4-02：输入 broker 与流式封装原型

1. 由 Host 创建只针对当前用户/任务/资产的 input broker 引用；worker 不能传入任意上游 URL，私密请求头留在 Host。
2. 解码器需要随机读时，broker 提供经验证的 Range；不可 Seek 的管道不冒充可随机定位输入。
3. 从候选 profile 中先选一种经目标浏览器验证的组合。AAC/fMP4、Opus/WebM 仅是候选，profile 必须包含具体 MIME、封装和传输方式。
4. 验证封装器能向非 Seek 输出发送初始化信息和后续音频；普通 MP4 需要尾部索引的行为不能当成流式成功。
5. 实际用较长输入确认全曲尚未处理完时浏览器已经可播放，同时检查没有整曲内存缓冲。
6. 此时未验证的时间定位声明 seekMode=none；原始资产的长度、ETag、Range 不能抄到处理输出。

测试：非 Seek 输入/输出、带长标签/索引的文件、损坏帧、慢浏览器、早取消、首帧超时。验收：正确声音在处理结束前出现，输出格式与描述一致。

## 3. P4B：把 PCM DSP 真正接入插件链

### S-P4-03：处理计划与 PCM 数据契约

1. 定义 PCM 采样格式、采样率、声道布局、帧数、时间戳/时间基准、缓冲归属和 end/error/discontinuity 标记。
2. 初始固定经过验证的一种 PCM 格式（例如交错 float32），不支持的布局显式转换或报错；不能根据字节数猜声道。
3. 控制 RPC 只传配置、开始/取消、reset、flush/drain 和状态；大块音频走独立有界二进制通道，不走 JSON/Base64。
4. 处理计划固定 sourceAsset、插件版本、参数、profile、执行端、输入和输出起点；生成 representationId 并记录 effectiveProcessing。
5. 允许原流、remux、完整处理不同路径；只有完整路径经过 PCM 时才可以插入本期 DSP。

### S-P4-04：首个可替换 DSP 节点

1. 在 Go 注册表提供单独增益节点，接口涵盖输入格式、输出格式、Process、Reset、Flush、延迟和参数校验。
2. decoder 输出 PCM → 该节点 → encoder/mux。不能只给 FFmpeg 添加一个滤镜参数就声称验证了独立 DSP 插件接口。
3. 使用已知波形或音量样本验证衰减、静音和多声道一致性；避免测试输入触发削波导致无法比较。
4. 记录节点分配/缓冲边界，按块处理并复用缓冲；无需因为 Go 有 GC 就否定服务端处理，但必须测吞吐、延迟和内存。
5. 必需 DSP 失败使尝试失败；只有明确可旁路的效果可降级，并把实际链反馈给终端。
6. 服务端不加入桌面切歌淡化，避免将 Client 专属效果固化进流。

测试：空块、末尾不足块、格式不匹配、参数非法、处理取消、节点失败、reset 后无残留状态。验收：输出声音和可测 PCM 变化符合配置，源文件摘要不变。

## 4. P4C：timeRestart 和时间映射

### S-P4-05：定位重建流程

1. seek 接受旧 attemptId、曲目 positionMs 和幂等键；校验范围、归属、最新代次和当前能力。
2. 计算输入定位点、解码预滚和丢弃帧范围，重置 DSP/编码器/封装状态；编码延迟和输出起点进入时间映射。
3. 返回新 attempt、representation、URL、positionOriginMs、有效 seekableRange 和必要映射；旧表示不能追加新数据。
4. 对同一 SourceTrack 的输出重建不要求已匹配 Recording；仅在输入版本/处理时间关系已验证的范围恢复。
5. seek/recover 沿 P3 的每会话串行变更与幂等规则执行；Web 收到上次操作确认的 attemptId 后才发送最新待定位目标。网络断开不代表操作撤销，同键重试不能另建输出。取消无消费者旧任务；晚完成的 FFmpeg 不能发布为最新输出。
6. 新输出未准备好或映射验证失败返回明确失败/重试/重播要求，不报告成功再让页面播放错误位置。

### S-P4-06：位置正确性与浏览器联调

1. 使用有不同时间段标记、前导静音和已知采样率的样本验证非零起点、尾部及多次定位。
2. 每个 profile 在验证记录中写明可达到的定位精度和实测误差，至少保证界面位置与实际声音一致；无法满足目标的 profile 不发布 seek 能力。
3. Web 用曲目位置控件请求 timeRestart，加载新流后按映射展示；不能把原曲位置直接设置给从零开始计时的新输出片段。
4. 有变速或非线性时间映射的 DSP 首版可返回不支持该组合的 seek，不假设 positionOriginMs 一个常量足以描述所有效果。
5. 严格区分原始流 byteRange 和处理流 timeRestart，缓存成完整稳定文件之后才能按已验证能力另发 byteRange 表示。
6. 验证快速连续定位、定位中恢复、响应丢失重试和定位时切歌；同曲后续请求引用已确认 attemptId，最终落点为最新意图，未知结果不会触发盲目重试。

## 5. P4D：取消、缓存和资源治理

### S-P4-07：有界资源与进程回收

1. 按实例/用户限制活跃处理数、排队数、PCM 队列、内存、CPU 使用策略、缓存与空闲时间。
2. 背压由 Gateway 经编码/DSP/解码传回来源，慢消费者不得让所有节点继续无限生产。
3. 取消、超时、浏览器断连及 Server drain 能终止整条子进程树、撤销 broker 能力并释放文件；Windows/Linux 分别验证退出行为。
4. 短暂重连宽限期有界，暂停与断开不能永久占用处理进程；多个请求共享尝试时用引用计数避免误杀仍有消费者的任务。
5. stdout/stderr、进程参数和错误日志不能泄露输入能力 token、私密请求头或源路径。

### S-P4-08：输出版本和缓存

持久处理输出缓存已实现并通过Windows/Linux真实Worker与播放race验证；完整发布、跨会话及重启复用、半成品拒绝、当前授权、共享配额和引用回收见[缓存说明与实测](../processed-cache.md)。命中仍为processed/timeRestart，不据缓存存在新增未验证的MP3字节定位能力。

1. 缓存键包含资产版本、用户/账号授权范围、插件/参数/profile 和生成起点；不同用户私有来源不能因 hash 相同就共享可读缓存。
2. 正在生成的输出与已校验完整输出分开；中断产物不标 complete，不提供伪造长度或稳定 ETag。
3. 写缓存走临时文件与发布状态，活动 attempt 保留引用；回收只清理没有引用且过宽限期的对象。
4. 命中缓存仍检查用户当前权限；源版本失效后未完成产物不能用于新资产。

## 6. P4 完成门槛

- [x] 非直放输入经 decoder→独立 PCM DSP→encoder/mux，实际Edge在全曲生成前解码出非零PCM。
- [x] MP3 profile的非零 timeRestart、频率标记、100ms容差及快速连续定位通过。
- [x] 原流路径仍可用，同一效果未两端重复，输入文件未修改。
- [x] 缺工具、崩溃、损坏输入、慢消费者、取消、重启和退出的现有处理资源回收通过。
- [x] 所测构建/profile/浏览器矩阵和限制已有记录，未支持能力明确返回。
- [x] S-P4-08持久输出缓存及其发布、复用、回收全链验收，限已验证mp3-128/gain-v1处理profile。
- [ ] 人工扬声器听音及其他浏览器/profile实测；不得从当前PCM证据推定。

已有证据见[插件音频验证](../validation/plugins.md)、[Linux实际音频](../validation/configuration-linux-audio.md)与[Web兼容记录](../../../web/docs/compatibility.md)。CPU、内存、文件句柄及并行后台任务的资源测量属于P8单独记录，不承诺跨机器性能。

依据：[FFmpeg 处理流程](https://ffmpeg.org/ffmpeg.html#Detailed-description)、[封装格式文档](https://ffmpeg.org/ffmpeg-formats.html)。具体 flags 由原型确定后记录，不把命令行示例当跨浏览器兼容证明。
