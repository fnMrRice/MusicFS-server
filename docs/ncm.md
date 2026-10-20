# NCM 解密插件

本插件已实现本地 `.ncm` 读取、内部MP3/FLAC原字节提取、扫描元数据及播放接入。核心、独立Worker、Server与最简Web已通过合成样本验证，后续真实本地库抽样包含4个NCM，见[真实库记录](validation/real-local-library.md)。本插件不负责账号登录或在线下载；另一个[网易公开资料插件](online.md)不提供完整音频，两者能力不混用。

## 1. 边界与参考

- 独立进程：`musicfs-plugins --plugin ncm`；插件标识 `musicfs.plugin.decoder.ncm`，角色 `audio`，能力 `audio.decrypt`。
- 核心采用 Go 标准库重写；参考 [taurusxin/ncmdump-go 的 MIT 实现](https://git.taurusxin.com/taurusxin/ncmdump-go/src/branch/main/ncmcrypt/ncmcrypt.go)及 [ncmdump.rs 的 MIT 实现](https://github.com/iqiziqi/ncmdump.rs/blob/master/crates/ncmdump/src/ncmdump.rs)。未引入这两个项目的运行依赖，也未移植 GPL 的 keuin/ncm 源码。
- NCM 是外层加密容器。解密结果保持内部编码字节，不将 MP3/FLAC 解码再压缩；后续仅在终端不兼容或用户要求 DSP 时进入已有 FFmpeg 管线。
- 原始文件只读，封面 URL 不下载，元数据不写回。封面数据跳过；曲名/艺术家可取自容器，时长/码率以内部音频的实际 ffprobe 结果为准。

## 2. 运行

运行 `scripts/build.ps1`（Linux为 `sh scripts/build.sh`）构建 `musicfs` 与 `musicfs-plugins` 两个程序；其中 `musicfs-plugins --plugin ncm` 是本功能入口。配置增加：

```yaml
# Under the top-level plugins sequence:
- pluginId: 'musicfs.plugin.decoder.ncm'
  executable: '../build/windows/musicfs-plugins.exe'
  args: ['--plugin', 'ncm']
  enabled: true
  instances:
    - instanceId: '96412f52-9ff4-5a82-9eaa-7644a5837cda'
      enabled: true
      config: {}
```

使用 `configVersion: 1` 的YAML，[正式示例](../configs/server.example.yaml)已包含该插件，不要重复添加相同ID；旧JSON配置不兼容。相对路径以配置文件目录为基准，Linux 使用对应无 `.exe` 程序。可选插件字段 `sha256` 固定管理员审核的Worker摘要；省略时采用与其他官方Worker相同的显式路径信任与本次启动摘要检查。插件与实例两层都启用才生效；设任一层 `enabled: false` 并重启即可停用NCM，其余格式仍可使用。配置读写与应用边界见[配置说明](configuration.md)。

配置后重启 Server，对音乐目录重新扫描。列表容器显示 `ncm`，codec 显示内部音频格式。点击播放无需额外选项：内部 MP3 可交付固定解密表示并提供 Range；内部 FLAC 按当前播放策略转为 MP3。勾选服务端处理时解密后照常应用 Go DSP。

需要 FLAC 转码和准确的媒体参数时，启用 `musicfs.plugin.decoder.ffmpeg` 插件及其音频实例，在实例 `config` 中配置 `ffmpegPath`、`ffprobePath`。仅有 NCM Worker 时可读取 MP3，未探测到的时长/码率保持未知。损坏或不支持的 NCM 返回明确错误，不作为一轮扫描成功空库。

## 3. 数据与接口

Local Files 扫描纳入 `.ncm`，根据 `CTENFDAM` 文件头识别容器。解密插件通过版本化 JSON-RPC 握手与建立任务，文件字节通过随机、任务限定的 loopback HTTP Range 通道传递，控制面不装载音频正文。

插件从输入句柄读取，不收到原始文件路径；解析过程限制长度、解密块与元数据大小，读取内容可随机访问。一次任务固定输入、资产和 nonce；关闭或取消后回收进程及能力 URL。官方插件仍是受信任程序，独立进程不等于完整操作系统沙箱。

播放创建时将解密内容写入已有私有磁盘快照，受缓存预算限制；快照发布前重新校验原始文件版本。SourceAsset 仍指原始 NCM，解密表示使用自己的长度、ETag、representationId 和 attempt URL。其 Range 不引用加密原文件的字节偏移。

HTTP 播放描述的 `effectiveProcessing` 增加 `{ "type": "ncmDecrypt", "gainDb": 0, "execution": "server", "version": "1" }`。其中 `gainDb: 0` 是既有模型的字段值，纯解密不执行增益 DSP；进入后续音频管线时另追加 `gain`，即使增益为 0。既有 Web 可忽略不认识的处理类型，无需新增端点或更改必填字段。纯解密交付为 `proxy/byteRange`；解密后转码为 `processed`，已知时长时支持 `timeRestart`，未知时长时为 `none`。插件 wire 字段见[协议说明](contracts/plugins/v1/README.md)。

每个解密任务独立进程，默认并发上限 2，启动及打开共限 5 秒，任务最长 2 分钟。Range 通道每段最多 64 KiB，验证精确长度、范围和 loopback 地址；禁止代理、重定向和压缩响应。播放快照拷贝完成后即关闭 Worker，后续播放使用该私有表示，不依赖 Worker 持续存活。

## 4. 验证状态

下表保留2026-10-09首次NCM增量的验证事实与当时四程序构建范围。2026-10-10后续YAML/Linux音频及真实库结果单列，不能将新结果追记为首次浏览器样本范围；完整阶段记录见[实施记录](validation/progress.md)。

| 验证层 | 实际结果 |
| --- | --- |
| Go 核心 | MP3/FLAC 原字节、随机及并发 ReadAt、SectionReader、EOF、元数据防御性复制、缺失标签、封面填充、截断/畸形块和不支持格式通过；核心定向 race 通过（2.199 秒） |
| 模糊测试 | `FuzzMalformedNCM` 单轮 `-fuzztime 10s -parallel 2` 执行 299,534 次，未发现崩溃；仅代表本轮输入与执行量 |
| 真实 Worker | MP3/FLAC 随机并发 Range、分块、跳过 3 MiB 封面、限额、损坏输入、取消/Close 回收、再次打开和二进制哈希拒绝通过 |
| 通道安全 | 非 loopback/错误 nonce/query/task、重复 open、重定向、200 代替 206、错误/重复 Content-Range、错误长度、短正文和压缩响应均被拒绝 |
| Server 端到端 | 启用/未启用插件、损坏文件、中文名、正确 Range、跨会话拒绝、幂等重放、原文件修改后的固定快照、撤销及普通 WAV 回归通过；大封面不占解密缓存预算，音频输出超预算返回 429 |
| 独立样本 | Web 的 Node 生成器独立封装 CC0 音频，不调用 Go 测试生成器；Go 核心逐字节还原 MP3 33,432 字节、FLAC 87,122 字节 |
| Edge 154.0.4258.62 | 三项真实浏览器联调通过（19.3 秒）：MP3 解密与精确 Range/非零 PCM；FLAC 解密转 MP3、4 秒定位后 660 Hz 标记；损坏 NCM 明确失败后普通 WAV 仍可播放 |
| Linux 原生 Worker | Arch WSL2 / Go 1.26.9 真实 NCM Worker 测试通过，8.622 秒；不依赖 FFmpeg |
| 本轮全量回归 | 配置真实 FFmpeg/ffprobe 后，`scripts/check.ps1 -Race` 的格式、vet、全部测试及 race 通过；四程序 Windows amd64 / Linux amd64 构建通过 |

浏览器合成样本、摘要、测量值与截图见[Web NCM记录](../../web/docs/ncm-validation.md)及[机器证据](../../web/docs/validation/ncm-edge-2026-10-09.json)。后续[真实库验收](validation/real-local-library.md)覆盖4个真实本地NCM的HTTP播放/定位/摘要不变；[Linux音频记录](validation/configuration-linux-audio.md)覆盖实际处理链。尚未人工扬声器试听，也未遍历其他浏览器、所有NCM变体或不同网易云客户端版本。

可重复执行：

```text
go test -mod=readonly ./tests/unit/ncmdecoder ./tests/contract/plugins
go test -mod=readonly ./tests/unit/ncmdecoder -run '^$' -fuzz FuzzMalformedNCM -fuzztime 10s -parallel 2
go test -mod=readonly ./tests/integration/plugins -run '^TestActualNCM' -count=1
go test -mod=readonly ./tests/e2e -run '^TestNCMServerRoundTripAndDisabledPlugin$' -count=1
```

浏览器测试在独立 Web 仓库执行 `npm run test:e2e -- ncm.spec.ts --max-failures=1`，按该仓库说明设置实际 FFmpeg/ffprobe 路径。进程集成与浏览器测试需要允许本机 loopback 通信；受限执行环境阻止连接时不能将启动超时当作格式兼容结论。

## 5. 手工验证

1. 按[运行说明](running.md)构建当前八程序制品，配置NCM Worker与音乐目录，重启Server后重新扫描。用测试目录或自己的本地NCM样本验证，保留原文件副本及摘要。
2. 登录 Web、刷新来源列表，确认 NCM 条目的容器为 `ncm`、codec 来自内部 MP3/FLAC；未配置 ffprobe 时，时长和码率可以未知。
3. 播放内部 MP3：页面应显示“服务端解密输出”，可以暂停、继续和停止；有时长时检查定位。开发者工具中输出为音频 MIME，Range 长度与解密表示一致。
4. 配齐 FFmpeg/ffprobe 后播放内部 FLAC：页面应显示“服务端解密并转码输出”。输入目标位置并定位，确认新片段开始；勾选既有 -6 dB 选项并重新选曲后再检查增益处理。
5. 验证损坏样本或暂时将 `musicfs.plugin.decoder.ncm` 插件的 `enabled` 改为false并重启：播放 NCM 应明确失败，普通 WAV 仍能播放。恢复配置后重新扫描、播放，核对源文件摘要未被修改。
