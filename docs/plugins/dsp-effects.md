# 常见 DSP 与声道转换

内置均衡器、压缩器、混响及声道转换随 Server 构建交付。服务端听感效果默认停用，Web 通过本地 DSP 实时调整参数；必要声道转换仍在 Server 执行。以下描述保留的服务端算法，须显式设 `audioDSP.enabled=true` 才会对服务端 DSP 会话启用；已有会话、跳转与恢复固定创建时设置。部署 YAML 需重启生效。执行边界见 [本地 DSP](../playback/local-dsp.md)，协议见 [DSP 插件](dsp-plugins.md)，接口见 [DSP 配置](dsp-settings.md)。

## 配置示例

以下片段合并进部署 YAML；先确保内置 FFmpeg 实例已配置有效工具路径。

```yaml
audioChannels:
  outputLayout: stereo # 可选 stereo、'5.1'、'7.1'
audioDSP:
  enabled: true
  chain:
    - name: gain
      version: 1
      params: {gainDB: -6}
    - name: equalizer
      version: 1
      params: {frequencyHz: 1000, q: 0.707, gainDB: 3}
    - name: compressor
      version: 1
      params: {thresholdDB: -18, ratio: 4, attackMs: 10, releaseMs: 100, makeupDB: 0, kneeDB: 6}
    - name: reverb
      version: 1
      params: {roomSize: 0.5, damping: 0.5, wet: 0.2}
  plugins: []
```

三个效果也接受正式名 `musicfs.dsp.equalizer`、`musicfs.dsp.compressor`、`musicfs.dsp.reverb`；直接写入 `chain`，不要在 `plugins` 中重复注册。短名与正式名均为保留名称，不允许外部程序覆盖。节点版本均为 `1`，当前发行版本均为 `0.1.0`。链最多 15 个配置节点，加用户增益后最多 16 个。

## 已实现效果

| 节点 | 参数（默认值） | 有效范围与行为 |
| --- | --- | --- |
| `equalizer` | `frequencyHz=1000`、`q=0.707`、`gainDB=0` | 20—20000 Hz；Q 0.1—20；增益 −24—24 dB。每节点一个峰值均衡频段，重复节点形成多段 EQ，各声道独立滤波状态 |
| `compressor` | `thresholdDB=-18`、`ratio=4` | 阈值 −60—0 dB；压缩比 1—20；按全部声道最大峰值产生同一个增益，保持声像关系 |
| `compressor` | `attackMs=10`、`releaseMs=100` | 起音 0.1—200 ms、释放 10—3000 ms；一阶时间常数，到达目标变化约 63.2%，无前视，不是硬限幅器 |
| `compressor` | `makeupDB=0`、`kneeDB=6` | 补偿 0—24 dB；软拐点宽度 0—24 dB，0 表示硬拐点 |
| `reverb` | `roomSize=0.5`、`damping=0.5`、`wet=0.2` | 三者均为 0—1；房间尺寸控制反馈 0.7—0.98，阻尼控制低通系数 0—0.4，wet 为干湿线性混合 |

效果支持 48 kHz 交错 float32 的单声道、立体声、5.1 和 7.1；当前播放输出提供后三种布局。混响对各非 LFE 声道使用独立延迟，LFE 原样通过，不添加混响。5.1 内部顺序为 FL、FR、FC、LFE、BL、BR，7.1 追加 SL、SR；Opus 封装的声道映射由编码阶段处理。

未知参数、非有限值和越界值均拒绝。实例间不共享历史状态；重新配置、Reset 和不连续标记会清空状态。各节点保持输入帧数、时间标记和采样率；混响在曲目原有时长内衰减，不追加曲尾。输出限制在 −1—1；EQ 提升或压缩器补偿可能触发削波，应先用负增益留出余量，节点顺序不会自动重排。

EQ 公式依据 [W3C Audio EQ Cookbook](https://www.w3.org/TR/2021/NOTE-audio-eq-cookbook-20210608/)；压缩曲线依据 [Giannoulis 等的动态压缩器设计论文](https://joshreiss.github.io/documents/2012/GiannoulisMassbergReiss-dynamicrangecompression-JAES2012.pdf)；混响采用 Schroeder 的梳状与全通结构，可参照 [STK FreeVerb](https://github.com/thestk/stk/blob/master/src/FreeVerb.cpp)。这些是本项目实现的参考，不表示导入第三方插件二进制。

## 声道转换的位置与边界

`musicfs.plugin.decoder.channelmix` 是独立的内置声道转换插件，在插件列表归入 DSP，源码位于 `plugins/dsp/channelmix`；保留原 ID 以兼容已有配置和处理身份。转换规则与效果节点分离。实际管线为：

**解码并重采样到 48 kHz → 保留源布局的 PCM → 声道转换 → 用户增益 → 配置 DSP 链 → 编码封装。**

对应 [PlantUML 管线图](../../../docs/diagrams/server-audio-channels.puml) 保留可编辑源码。

声道转换放在效果链前，使所有效果按同一最终布局处理。5.1/7.1 输入不会先压为双声道再扩回环绕声。后续如果引入只支持立体声的第三方效果，需要显式协商转换位置与信息损失，不能自动在两侧插入隐式下混/上混。

同一个插件负责下混与声道扩展，无需为 5.1、7.1 再建插件。输入接受 mono、stereo、5.1、5.1(side)、7.1；从 WAVE 格式与声道掩码识别布局，不凭 6/8 个声道猜位置。未支持或不明确的多声道布局明确失败。下混使用中心/环绕权重与按行归一化，LFE 不混入立体声；同布局保留各声道；扩展布局保持已有声道，空位补静音，单声道转立体声复制到左右。立体声扩为环绕布局不等于产生独立录制的真实环绕声。

效果链也可插入 `channelmix`（别名 `musicfs.dsp.channelmix`，兼容历史插件 ID）并调整其顺序，参数 `outputChannels` 接受 1、2、6、8，默认 2。此节点可以改变中间 PCM 布局，但不改变帧数和采样时间；链末输出必须回到所选编码布局。播放入口仍先自动适配所选输出布局，页面预览明确展示这之后各节点的输入、输出和可继续处理标记。

DSP 的新输入输出协商允许声道布局变化，编码封装和采样率转换仍由独立处理阶段负责。插件以 `DescribeIO` 提供无副作用的声明，以 `ConfigureIO` 配置实际输入并返回输出格式及 `CanContinueDSP`；不可继续处理的输出后禁止再接 DSP。旧外部 MFD1 v1 继续按固定双声道契约校验，不因新声明而获得多声道能力。

| `audioChannels.outputLayout` | 实际输出 | 客户端要求 |
| --- | --- | --- |
| `stereo`（默认） | `mp3-128`：双声道、48 kHz、128 kbit/s MP3 | 支持 `audio/mpeg` |
| `'5.1'` | `opus-5.1`：6 声道、48 kHz、256 kbit/s Opus/WebM | 支持 Opus/WebM |
| `'7.1'` | `opus-7.1`：8 声道、48 kHz、384 kbit/s Opus/WebM | 支持 Opus/WebM |

默认立体声只在需要处理的播放中转换，兼容原流仍可直接交付。配置 5.1/7.1 会强制处理并要求内置音频实例、可用 libopus 编码器和 WebM 封装；依赖缺失时启动失败，不退回原流或双声道。浏览器格式探测只能确认解码可能性，不能证明操作系统、声卡或音箱最终输出了独立 6/8 声道；物理环绕声仍需对应设备验收。

## 定位、缓存与兼容性

有状态 DSP 每次播放尝试独立创建。seek 或恢复时，从曲首解码并重建全部历史状态，DSP 处理后按帧丢弃目标位置之前的内容，再开始编码；这样同一输入同一处理配置在目标位置与连续播放一致。处理时间随定位位置增长，尚未提供状态检查点。纯无状态链保留快速定位路径。

当前仍使用 `timeRestart`：客户端加载新的受鉴权流地址，不沿用旧 Range。输出布局、编码 profile、声道转换发行版本、音频 Worker 身份、DSP 顺序及参数均进入表示/缓存/恢复身份。包括空 DSP 链的处理路径也使用 `dsp-v1:<SHA256>` 修订；旧 `gain-v1` 仅兼容读取，旧时间轴证据不能直接替代新处理身份。

可移植备份恢复会将声道输出重置为默认 `stereo`，并与原有策略一起停用音频实例、清除处理缓存；恢复后须重新确认工具与布局配置，避免在未配置音频实例时强制环绕声。

FFmpeg Worker 升至 `0.5.0`，Host 精确校验发行版本。HTTP `outputProfile` 和 `effectiveProcessing` 报告实际输出与处理顺序；旧客户端可以忽略新增处理名称，但必须正确协商 MIME。Web 已探测 Opus/WebM；本次没有承诺所有浏览器与音响设备均能输出物理环绕声。

外部 MFD1 DSP v1 保持无状态、固定双声道、零延迟、无追加尾音的原约束，多声道管线拒绝使用这些外部节点。内置新效果直接调用 Go 接口，不把其有状态、多声道能力冒充成 MFD1 v1 的扩展。原官方 gain 在多声道管线使用同一内置增益工厂。

## 验证范围

本次在 Windows、FFmpeg/ffprobe 6.1.1 下验证：

- EQ 实测中心频率增益、压缩曲线与时间常数、混响衰减，以及所有布局的分块一致性、状态隔离、重置和参数边界。
- 实际逐声道音段在下混、上混和同布局输出中的路由与幅度；未知多声道布局拒绝；Opus 6/8 声道编码后的声道位置。
- MP3 的有状态定位与完整处理后裁切参考逐字节相同；Opus/WebM 比较解码后的 PCM，逐样本一致，不要求封装随机标识相同。
- HTTP 协商、描述、缓存读取与重启恢复、seek、跨源证据恢复及布局变化后的隔离；实际 YAML 配置启动与重启后的六声道播放。

对应测试入口为 `plugins/dsp/effects`、`plugins/dsp/channelmix`、`plugins/decoder/ffmpeg`、`tests/integration/playback/channel_profiles_test.go` 和 `tests/e2e/yaml_surround_test.go`。真实音频测试须设置 `MUSICFS_FFMPEG`、`MUSICFS_FFPROBE`，未设置时的跳过不计验证。其他操作系统、所有浏览器和物理环绕声设备仍需分别验收。
