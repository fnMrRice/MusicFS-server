# Server DSP 插件

官方内置 DSP 已提供增益、参数均衡器、压缩器、混响和可排序声道转换，支持 mono、stereo、5.1、7.1 PCM 的逐节点输入输出协商。播放入口先自动适配所选布局，链末须匹配编码输出；节点可声明输出不能继续 DSP，此时禁止追加后继。内置有状态效果通过从曲首重建历史支持定位，保持帧数、零算法延迟且不追加曲尾。显式登记的第三方外部 DSP v1 仍仅支持无状态双声道节点。参数与格式见[常见 DSP 与声道转换](dsp-effects.md)，页面、个人覆盖和全局默认见[DSP 配置](dsp-settings.md)。当前运行验证范围为 Windows；其他平台和任意第三方算法需分别验证。

官方音频 Worker 实现版本为 `0.4.0`。Host 握手要求该精确版本，旧 `0.1.0`、`0.2.0`、`0.3.0` 在音频请求前被拒绝，避免遗漏 DSP、声道转换或状态重建。内置音频实现随 Server 升级；显式外部进程模式需要配套升级 `musicfs-plugins`（或独立入口）并更新固定摘要。控制协议仍为 1.0，独立 MFD1 DSP 帧协议仍为 v1；内置有状态、多声道效果不使用该帧协议。

内置和外部 DSP 的 ready 响应都必须提供 `pluginVersion`，使用最多 128 字符的有效 SemVer，缺失、空白或无效值均被宿主拒绝；Worker 的 `Serve` 入口也在初始化节点前校验版本。该发行号与整数 `protocolVersion`、`nodeVersion` 分离。官方 gain 使用 [sdk/bundledplugin/releases.go](../../sdk/bundledplugin/releases.go) 中的 `GainVersion`，当前为 `0.1.0`。内置 DSP 版本随 Server 构建升级；显式外部节点须同步升级程序及固定摘要。旧节点即使帧协议仍为 v1，也会因缺少发行版本被拒绝。

## 配置与执行

Server 的 `audioDSP` 与 `audioChannels` 是部署配置，修改后重启生效。先转换到目标声道布局，再执行播放用户偏好的 gain 和 `chain` 数组中的节点；节点顺序会影响削波等结果，不自动排序或合并。配置非空链或环绕声输出时，即使原曲格式可直接播放，也进入 `processed` 路径。立体声要求终端支持 MP3，5.1/7.1 要求 Opus/WebM。

```yaml
audioDSP:
  chain:
    - name: gain
      version: 1
      params:
        gainDB: -3
    - name: musicfs.dsp.gain
      version: 1
      params:
        gainDB: -6
  plugins:
    - name: musicfs.dsp.gain
      timeoutMs: 5000
```

`plugins` 注册节点，官方 `musicfs.dsp.gain` 规范化为内置实现，不需要可执行文件和摘要；只有出现在 `chain` 中的节点才参与播放。同名节点可在链中出现多次，各自使用独立实例。以下路径及摘要规则仅针对显式配置的第三方外部节点。`executable` 相对 YAML 文件所在目录解析；宿主发送给音频 Worker 前转换为绝对路径。SHA-256 在配置加载时规范为小写，并在每次启动 DSP 进程前核验。

| 字段 | 约束 |
| --- | --- |
| `chain` | 最多 15 个节点，加上用户 gain 后总计最多 16 个 |
| 节点 `name` | 已注册名称；匹配 `[a-z][a-z0-9_.-]{0,63}` |
| 节点 `version` | 必填整数，当前仅接受 `1` |
| 节点 `params` | 可省略；最多 64 个有限数值参数，键长 1—64 |
| `plugins` | 最多 16 个注册项，名称唯一，不能覆盖 `gain` 或 `bypass` |
| 外部 `executable/sha256` | 必填；部署方提供可执行文件与对应 SHA-256 |
| 外部 `args` | 可选；最多 32 个参数，每项最多 4096 字符 |
| 外部 `timeoutMs` | 每次握手/处理/控制操作的超时；省略或 0 为 5000，显式非零值为 10—30000 毫秒 |

内建 `gain` 仅接受 `gainDB`，范围 -96—12 dB；`bypass` 不接受参数。`equalizer`、`compressor`、`reverb` 及其 `musicfs.dsp.*` 正式别名无需 `plugins` 注册，参数见[效果表](dsp-effects.md#已实现效果)。这些名称同样不允许被外部插件覆盖。HTTP 用户偏好仍为 `gainDb`，原有 -60—0 dB 边界不变。配置中的外部路径、启动参数和可执行摘要不会出现在 Web 播放描述中。

Web 提供 DSP 配置页面，内置节点与已经部署注册的外部节点可按服务端声明编辑；外部程序仍仅由部署文件注册，没有 DSP 商店、上传安装或自动发现。个人和全局设置保存后在下一次播放生效，所有选中节点均必需，运行失败会中止处理，不自动旁路。非空部署链要求音频实例启用；若启动时处理器或必需 DSP 不可用，Server 拒绝启动。内置 DSP 还要求音频插件使用内置执行；显式外部 FFmpeg 插件配内置 DSP 时返回 `builtin DSP requires builtin audio execution`，不会把官方 gain 放到插件子进程。执行权限与工具设置还须满足[配置说明](../operations/configuration.md)。

插件列表固定展示官方 `musicfs.dsp.gain`，`stateMutable=false`、`instances=[]`，不通过普通实例启停或配置接口修改。其 `dsp.configured` 表示已注册官方 gain 或在启动链中使用内建/官方 gain；`chainState` 为 `not_configured`（未配置）、`not_in_chain`（已注册但未加入链）、`in_chain`（已加入启动链）。`nodes` 汇总内建 `gain` 与官方 `musicfs.dsp.gain`，保留实际 `mode=builtin/process`（官方 gain 为 builtin）、完整原链从 1 开始的位置、节点协议版本及数值参数。重复节点保序，其他节点造成的位置间隔不会被重排；用户偏好增益及 `bypass` 不另列插件。

这些字段描述本次启动配置，不表示当前正在播放或有 DSP 进程运行。`configuredEnabled` 对此行仅表示已加入配置链。发行版本默认来自官方共享声明；音频处理器现有初始化握手已验证官方 gain 时优先显示该发行版本，读取列表不会启动额外进程。任意第三方 DSP 尚无静态发行声明，当前不为其生成独立管理插件条目，也不冒用官方 gain 的版本；本展示不等同于完整第三方 DSP 目录。投影不含程序路径、启动参数、摘要或凭据，配置修改仍须编辑部署文件并重启 Server。

均衡器、压缩器和混响同样有独立的只读管理条目，分别汇总短名和正式别名的节点位置、版本与参数；发行号来自内置实现声明。声道转换插件在内置算法目录中展示支持布局，实际输出布局以播放描述为准。详情见[常见 DSP 与声道转换](dsp-effects.md)。

## 构建并使用示例

官方 `musicfs.dsp.gain` 在播放链中直接使用原生增益工厂，支持协商声道和较大缓冲区，不启动插件进程；节点版本为 1，参数与内建 gain 相同。可选独立入口 [musicfs-plugin-dsp-gain](../../plugins/dsp/gain/cmd/musicfs-plugin-dsp-gain/main.go) 保留原 `MFD1` 协议；使用其单独文件时省略角色参数。该入口用于显式外部兼容或协议测试，默认部署不执行它；部署配置中的官方 gain 仍映射为内置实现。

需要构建兼容入口作独立协议验证时，在 Server 仓库根目录使用 PowerShell：

```powershell
go build -o ./bin/musicfs-plugins.exe ./src/app/musicfs-plugins
(Get-FileHash -Algorithm SHA256 -LiteralPath ./bin/musicfs-plugins.exe).Hash.ToLowerInvariant()
```

外部兼容测试使用输出摘要；默认官方 gain 配置不需要该文件。音频处理器及 FFmpeg/ffprobe 工具仍须按原要求可用。示例中的 `../bin` 适用于 YAML 保存在仓库 `configs/` 下的情形；部署目录不同时调整路径。非 Windows 平台应重新构建对应可执行文件并更新路径、摘要，不能沿用 Windows 二进制。

Go 插件入口调用：

```go
dspworker.Serve(ctx, os.Stdin, os.Stdout, name, "0.1.0", factory)
```

`name` 和 `version` 参数均为字符串；示例中的 `"0.1.0"` 应替换为插件自己的有效发行版本，官方 gain 两个入口共用 `bundledplugin.GainVersion`。`factory` 类型为 `audio.Factory`，从 `map[string]float64` 参数创建 `audio.Node`。节点须额外实现 `Stateless() bool` 并返回 `true`；`DelayFrames()` 必须为 0。宿主在握手时配置固定 PCM 格式，调用 `Process/Reset/Drain`，退出时调用 `Close`。完整入口见上述示例，Go 辅助包位于 [src/playback/audio/dspworker](../../src/playback/audio/dspworker)。

其他语言可按下述帧协议编写独立程序。当前没有已验证的 C++ DSP 示例；Go 接口本身不是跨语言 ABI。

## DSP v1 帧（仅旧增益与外部无状态节点）

这是音频执行器与 DSP 节点之间的专用协议，不使用 Provider/Scraper JSON-RPC。内置节点使用双向内存流；外部节点的 stdin 接收请求，stdout 只输出协议帧，英文日志写 stderr。每条连接同一时间仅有一个在途请求，响应的 kind 必须与请求一致；处理完成后才发送下一块，因此背压直接传回解码链。

每帧以 12 字节头部开始：

| 偏移 | 长度 | 内容 |
| --- | --- | --- |
| 0 | 4 | ASCII `MFD1` |
| 4 | 4 | little-endian uint32 `kind` |
| 8 | 4 | little-endian uint32 payload 长度，最大 65536 字节 |
| 12 | payload 长度 | 正文 |

未知 magic、超限长度、截断帧、响应种类不匹配均失败。节点从首帧开始等待 hello；不能在握手前发送 PCM。只有握手和错误正文采用 UTF-8 JSON，PCM 从不编码为 JSON 数组或 Base64。

| kind | 请求与响应 |
| --- | --- |
| 1 / hello | 请求提供协议、身份、格式、参数；响应声明同一身份与能力 |
| 2 / process | 请求和响应均为下述 PCM 块 |
| 3 / reset | 空正文请求，空正文响应 |
| 4 / drain | 空正文请求，空正文响应；v1 无额外尾音输出 |
| 5 / error | 插件失败；SDK 发出受限错误 JSON 后结束，宿主中止任务 |

hello 请求示例：

```json
{
  "protocolVersion": 1,
  "name": "musicfs.dsp.gain",
  "nodeVersion": 1,
  "format": {"sampleFormat": "f32le", "sampleRate": 48000, "channels": 2, "layout": "stereo"},
  "params": {"gainDB": -6}
}
```

hello 响应必须包含：

```json
{
  "protocolVersion": 1,
  "name": "musicfs.dsp.gain",
  "nodeVersion": 1,
  "format": {"sampleFormat": "f32le", "sampleRate": 48000, "channels": 2, "layout": "stereo"},
  "pluginVersion": "0.1.0",
  "delayFrames": 0,
  "tailFrames": 0,
  "preservesTiming": true,
  "stateless": true
}
```

宿主核验协议版本、节点名称/版本、插件发行版本、格式及上述能力；成功握手后可通过 `Process.PluginVersion()` 读取已验证发行版本。名称必须与配置注册项精确一致；不能仅声明兼容版本范围或任意其他 PCM 格式。外部节点的 SHA-256 由宿主读取可执行文件核验，不依赖插件自报；内置节点使用受信任的编译内实现身份。

## PCM 块与时间语义

process 正文前 24 字节为块描述，剩余正文是交错双声道 `f32le` 样本：

| 偏移 | 类型 | 含义 |
| --- | --- | --- |
| 0 | little-endian uint64 | `sequence` |
| 8 | little-endian int64 | `positionFrames` |
| 16 | little-endian uint32 | `frames`，范围 0—4096 |
| 20 | little-endian uint32 | flags：bit 0 为 End，bit 1 为 Discontinuity，其余位必须为 0 |
| 24 | `frames * 2` 个 float32 | 左右声道交错的 PCM，所有样本必须为有限值 |

正文长度必须等于 `24 + frames * 2 * 4`。响应必须原样保留 sequence、positionFrames、frames 和 flags，只允许修改样本值。宿主拒绝 NaN/Infinity、越界块大小、非零保留位和时标改变。End 是块结束提示，完整结束以 drain 操作为准，插件不能假设最后一块必然带 End。

v1 算法必须无状态：结果不依赖之前的 PCM 块、播放起点或 Reset 前的历史；同一段输入按不同块大小切分仍应得到相同结果。仅申报零延迟并不足以满足本约束。节点不得引入尾音、预滚需求、重采样、声道转换、变速或有状态滤波。声明由宿主检查，算法是否遵守约束须由插件作者以分块/定位测试证明。

每个处理 attempt 重新创建链和节点上下文；仅外部节点另建进程。纯无状态链使用 `timeRestart` 从请求曲目时间重建；包含内置有状态节点时从曲首处理历史、处理后裁切至目标时间。外部 v1 本身不因此获得有状态能力；不同来源能否续播仍取决于独立的资产时间轴证据。

## 生命周期、描述与缓存

部署参数在处理器创建时深复制。既有会话持有当时的处理器，后续 seek 保留同一配置链。`effectiveProcessing` 按实际执行顺序给出用户 gain、各配置节点及其版本/参数；不公开可执行路径、参数列表或校验摘要。

所有处理路径使用 `dsp-v1:<SHA256>`，包括无额外效果的默认链；摘要绑定协议版本、声道转换发行版本、输出布局、有序节点及参数、注册配置、内置或外部执行身份及实现版本。此修订进入处理缓存和恢复 profile；布局、链、参数或执行身份变化不能复用旧处理表示，旧跨源对齐也不能直接沿用。`gain-v1` 仅用于兼容历史数据。FFmpeg 摘要与用户 gain 继续分别纳入现有表示身份。

指纹的 `audio.normalize` 不创建播放 DSP 链，仍输出 DSP 前的固定规范化 PCM，不受上述配置或用户 gain 影响。

外部操作超时、进程退出、协议违例或上游取消时，宿主关闭管道、回收进程并等待退出；Windows 使用 Job Object，Linux/macOS 使用进程组清理。失效输出不发布为完整缓存。每个节点/attempt 独立进程的成本需要按部署实际测量，当前未承诺任意第三方节点的吞吐或 CPU/内存上限。

官方内置节点取消时关闭内存流、取消上下文并等待处理退出，属于协作取消。其代码与 Server 共享地址空间，不能安全强杀某个插件线程，也不保证崩溃或卡死后其他业务继续运行。实例和任务仍独立，不因共用进程而共享参数、账号或播放状态。

可执行校验和进程回收不构成操作系统沙箱。配置的程序按服务账户权限运行；路径、参数和摘要属于管理员部署边界。不要将运行不受信任二进制视为已具备恶意代码隔离。

真实播放回归需要设置 `MUSICFS_FFMPEG` 与 `MUSICFS_FFPROBE`，执行 `go test ./tests/integration/playback -run TestDSPActualPlaybackCacheSeekRecoveryAndFingerprintIsolation -count=1`；未设置工具时跳过，不能计为运行验证。
