# NCM 解密插件

本插件已实现本地 `.ncm` 读取、内部 MP3/FLAC 原字节提取、外层封面附件、扫描元数据及播放接入。本插件不负责账号登录或在线下载；另一个[网易在线来源插件](../providers/online.md)不提供完整音频，两者能力不混用。

## 1. 边界与参考

- 默认主进程内置运行；`musicfs-plugins --plugin ncm` 仅供显式外部模式。插件标识 `musicfs.plugin.decoder.ncm`，角色 `audio`，能力 `audio.decrypt`。
- 核心采用 Go 标准库重写；参考 [taurusxin/ncmdump-go 的 MIT 实现](https://git.taurusxin.com/taurusxin/ncmdump-go/src/branch/main/ncmcrypt/ncmcrypt.go)及 [ncmdump.rs 的 MIT 实现](https://github.com/iqiziqi/ncmdump.rs/blob/master/crates/ncmdump/src/ncmdump.rs)。未引入这两个项目的运行依赖，也未移植 GPL 的 keuin/ncm 源码。
- NCM 是外层加密容器。解密结果保持内部编码字节，不将 MP3/FLAC 解码再压缩；后续仅在终端不兼容或用户要求 DSP 时进入已配置处理器，默认实现为 FFmpeg。宿主装配规则见[音频插件边界](plugin-boundaries.md)。
- 原始文件只读，封面 URL 不下载，元数据不写回。外层封面作为独立有界附件输出，不写入解密后的 MP3/FLAC；曲名/艺术家可取自容器，时长/码率以内部音频的实际 ffprobe 结果为准。
- NCM 插件只识别容器内封面的位置与长度，不解码图片。宿主将附件交给通用本地 Scraper 验证并生成候选；MP3/FLAC 自身的标签封面仍由同一 Scraper 提取，缩略图仍由现有内容变换插件处理。

## 2. 运行

运行 `scripts/build.ps1`（Linux 为 `sh scripts/build.sh`）构建 Server，NCM 实现已编入主进程，无需插件程序；兼容入口见[部署清单](../operations/deployment.md)。配置增加：

```yaml
# Under the top-level plugins sequence:
- pluginId: 'musicfs.plugin.decoder.ncm'
  execution: builtin
  enabled: true
  instances:
    - instanceId: '96412f52-9ff4-5a82-9eaa-7644a5837cda'
      enabled: true
      config: {}
```

使用 `configVersion: 1` 的 YAML，[正式示例](../../configs/server.example.yaml)已包含该插件，不要重复添加相同 ID；旧 JSON 配置不兼容。只有显式 `execution: process` 才需要 executable、角色参数和对应程序摘要，旧文件字段在内置模式不会被执行。插件与实例两层都启用才生效；设任一层 `enabled: false` 并重启即可停用 NCM，其余格式仍可使用。配置读写与应用边界见[配置说明](../operations/configuration.md)。

配置后重启 Server，对音乐目录重新扫描。列表容器显示 `ncm`，codec 显示内部音频格式。点击播放无需额外选项：内部 MP3 可交付固定解密表示并提供 Range；内部 FLAC 在客户端声明支持 `audio/flac` 且无处理要求时也直接交付 FLAC 原字节并提供 Range，不支持时转为兼容输出。勾选服务端处理时解密后照常应用 Go DSP。

需要音频转码和探测码率等媒体参数时，启用 `musicfs.plugin.decoder.ffmpeg` 插件及其音频实例，在实例 `config` 中配置 `ffmpegPath`、`ffprobePath`。仅有 NCM Worker 时可直接交付客户端支持的 MP3/FLAC；FLAC 的声道、采样率与已知时长可从结构校验获得，未探测到的码率保持未知。损坏或不支持的 NCM 返回明确错误，不作为一轮扫描成功空库。

## 3. 数据与接口

Local Files 扫描纳入 `.ncm`，根据 `CTENFDAM` 文件头识别容器。解密插件通过版本化 JSON-RPC 握手与建立任务，文件字节通过随机、任务限定的 loopback HTTP Range 通道传递，控制面不装载音频正文。

插件从输入句柄读取，不收到原始文件路径；解析过程限制长度、解密块与元数据大小，读取内容可随机访问。一次任务固定输入、资产和 nonce；关闭或取消后回收上下文、内存流和能力 URL，只有外部模式回收插件进程。内置代码共享 Server 地址空间，取消依靠协作，不提供崩溃隔离；外部进程也不等于完整操作系统沙箱。

容器的 `coverCapacity` 决定音频起点，`coverLength` 决定实际封面读取范围；容量中的剩余填充不能成为封面正文。`ncm.open` 在有封面时返回可选 `artwork: {url, size}`，封面服务使用独立的随机 loopback 端口，URL 固定为 `http://127.0.0.1:<port>/<nonce>/artwork`，`size` 等于有效封面长度。音频与封面共享任务绑定和生命周期，关闭时一并回收 input、output 及可选 artwork 通道；读取封面不需要先解密或复制整首音频。

音频 SDK 将封面表示为 `Attachment{Kind, Reader, Size}`，宿主汇集各解码层的附件，经授权资源传给本地 Scraper。封面资源使用 `kind=artwork`、`match=containerEmbedded`，候选记录 `origin=container`、`basis=containerEmbedded`，与音频标签封面和旁置图片分别保留来源。图片须通过现有类型、尺寸、摘要与资产版本检查后，才可预览、选用或自动补缺；完整流程见[本地内容补全](../metadata/metadata.md)。

播放创建时将解密内容写入已有私有磁盘快照，受缓存预算限制；快照发布前重新校验原始文件版本。SourceAsset 仍指原始 NCM，解密表示使用自己的长度、ETag、representationId 和 attempt URL。其 Range 不引用加密原文件的字节偏移。

HTTP 播放描述的 `effectiveProcessing` 增加 `{ "type": "ncmDecrypt", "gainDb": 0, "execution": "server", "version": "1" }`。其中 `gainDb: 0` 是既有模型的字段值，纯解密不执行增益 DSP；仅在服务端音效启用且会话归属为 server 时追加 `gain`；client 或部署禁用时不执行任何增益。既有 Web 可忽略不认识的处理类型，无需新增端点或更改必填字段。纯解密交付为 `proxy/byteRange`；解密后转码为 `processed`，已知时长时支持 `timeRestart`，未知时长时为 `none`。插件 wire 字段见[协议说明](../contracts/plugins/v1/README.md)。

每个解密任务拥有独立执行上下文和控制流，默认并发上限 2，启动及打开共限 5 秒，任务最长 2 分钟。显式外部模式才为任务创建进程。Range 通道每段最多 64 KiB，验证精确长度、范围和 loopback 地址；禁止代理、重定向和压缩响应。播放快照拷贝完成后关闭执行上下文，后续播放使用该私有表示，不依赖解密执行器持续存活。

## 未验收范围

测试使用合成 NCM 容器与生成的图片，不等于真实客户端下载文件的封面验收。尚未完成扬声器人工试听、其他浏览器、所有 NCM 变体及不同网易云客户端版本验收。NCM 本地解密不扩展在线账号或整曲下载能力。
