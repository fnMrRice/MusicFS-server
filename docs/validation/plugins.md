# 插件、受限来源与音频链验证记录

日期：2026-10-09。Windows amd64 及 Arch Linux WSL2 amd64 均使用 Go 1.26.9；Linux 的协议、来源和真实 Local Files Worker 测试已通过，真实音频测试结果分开标记。本文记录插件侧已验证结果，真实浏览器结果由联合验收记录补充；管线测试成功不等于浏览器已发声。

## 1. 实现与提交边界

| 步骤 | 生产文件与行为 | 提交标题（稳定检索项） |
| --- | --- | --- |
| PL-P0-01 | `src/pluginproto/manifest.go`、`provider.go`：版本、实例、角色、观测与资源 DTO | `feat(PL-P0-01): 定义版本化插件清单与来源协议` |
| PL-P0-02 | `envelope.go`、`framing.go`：1 MiB UTF-8 Content-Length 帧、业务错误；后续 `peer.go` 实现双向调用、取消和重复终态拒绝 | `feat(PL-P0-02): 实现有界插件消息帧与契约测试` |
| PL-P0-03 | `resources.go`：受控枚举与宿主命名空间状态 DTO | `feat(PL-P0-03): 定义受限资源与宿主状态协议` |
| PL-P1-01 | `src/pluginhost/registry.go`：绝对可执行路径、SHA-256、停用检查；启动不经过 Shell；主配置支持管理员固定 Worker 摘要 | `feat(PL-P1-01): 固定受信任插件路径与校验和`；`feat(PL-P1-01): 支持管理员固定 worker 摘要` |
| PL-P1-02 / PL-P2-01 | `pluginhost/process.go`、`resources.go`、`state.go`；Local Files 正式入口与测试进程 | `feat(PL-P2-01): 建立真实 Local Files worker 与持久来源身份`；`fix(S-P2-01): 加固真实插件握手取消与故障回收` |
| S-P2-02 / PL-P2-02 | `src/sourceaccess`：`os.Root`、句柄身份、版本、scope、过期、受限资源 | `feat(S-P2-02): 统一受限根与版本绑定的来源读取` |
| PL-P2-03 / PL-P2-04 | `src/plugins/localfiles`：宿主持久分配随机 ID、分页、确定改名保留 ID、基础 WAV/MIME 观测 | 同 Local Files 提交；完整元数据由下一项补充 |
| S-P2-04 | `src/plugins/audioffmpeg/metadata.go`：受控本机输入通道与 ffprobe 元数据 | `feat(S-P2-04): 通过受限 broker 探测音频元数据与工具能力` |
| PL-P4-01 / S-P4-01 | `audioffmpeg/capabilities.go`：实际工具版本、构建、编码器/解码器/封装器、固定输出组合 | 同 S-P2-04 的元数据与工具能力提交 |
| PL-P4-02 / S-P4-03 / S-P4-04 | `src/audio`、`audio/dsp/gain.go`、`audioffmpeg/pipeline.go`：真实独立 Go PCM 节点 | `feat(S-P4-03): 定义 PCM 数据边界与独立 Go 增益节点`；`feat(S-P4-04): 将 Go DSP 接入解码编码流式管线` |
| PL-P4-03 / S-P4-02 / S-P4-07 | `audioffmpeg/processor.go`、`worker.go`、`process_*.go`、独立音频入口：任务通道、背压与进程树回收 | `feat(S-P4-02): 通过任务限定二进制通道运行音频 worker` |
| PL-P4-04 / S-P4-05 | `tests/integration/plugins/audio_test.go`：真实转码、增益、非零时间定位与中途断连 | `test(S-P4-06): 验证增益定位精度与处理流首块时序` |
| S-P4 输入边界补充 | `metadata.go`、`pipeline.go` 限定实际音频 demuxer；伪 HLS 不得访问远端 | `fix(S-P4-01): 限定音频解封装器并拒绝外部播放列表` |

表中保留首次实现及主要验证的提交标题，可按阶段号或标题检索；不固定补签时会变化的提交摘要。S-P4-08 的资产/权限/输出缓存归属 Server 播放模块。本文第 1—5 节保留最初插件基线的实测结果；后续独立 NCM Worker、依赖升级及最终全量回归见[实施记录第 4 节](progress.md#4-后续授权增量独立-ncm-插件)。

2026-10-10的Linux真实音频补验单列在本文第6节及[配置Linux音频记录](configuration-linux-audio.md)，不覆盖下文旧轮次的缺工具与跳过事实。

## 2. 实际工具和输出组合

- FFmpeg / ffprobe：`6.1.1-full_build-www.gyan.dev`，GCC 12.2.0；本机已有安装，通过受控绝对路径调用。
- 构建包含 `--enable-gpl --enable-version3 --enable-shared`；探测确认 `libmp3lame`、`pcm_f32le`、WAV demux 和 MP3 mux。
- FFmpeg SHA-256：`fc635444b2a75d3c709b586ddd1e51e2d7c5b74126c7903db9658592af872ba7`。
- ffprobe SHA-256：`05bf9449818a2a0c76fabc332ae314d202dea8d10143934b1d9dfe9da2d672dd`。
- 本仓库不分发该 FFmpeg 二进制。打包分发时仍需整理具体构建的许可证材料与动态库清单；当前摘要核验只覆盖指定可执行文件。
- 唯一处理 profile 为 `mp3-128`：MP3 容器、`audio/mpeg`、48 kHz、双声道、128 kbit/s。编码器禁用需回写头部的 Xing 索引，输出支持非 Seek 管道。
- 完整路径为 FFmpeg 解码到交错双声道 float32 little-endian → Go 注册表的 gain 节点 → FFmpeg libmp3lame 编码/MP3 封装。未用 FFmpeg 滤镜代替独立 Go DSP。
- 原输入始终只读。处理输出不继承原输入的 Content-Length、ETag 或字节 Range。

## 3. 已运行测试与测量

全部测试位于仓库根 `tests`；生产文件均低于 400 行。

```powershell
$env:MUSICFS_FFMPEG = '<controlled absolute ffmpeg.exe path>'
$env:MUSICFS_FFPROBE = '<controlled absolute ffprobe.exe path>'
go test -count=1 -v ./tests/unit/plugins ./tests/contract/plugins ./tests/integration/plugins
go vet ./src/pluginproto ./src/pluginhost ./src/sourceaccess ./src/plugins/... ./src/audio/...
$env:GOOS = 'linux'
$env:GOARCH = 'amd64'
go build ./src/pluginproto ./src/pluginhost ./src/sourceaccess ./src/plugins/... ./src/audio/... ./src/app/musicfs-plugin-local ./src/app/musicfs-plugin-audio-ffmpeg
```

命令全部通过；使用的 Windows 沙箱阻止 FFmpeg 访问本机输入通道，真实音频集成测试在提升后的本地执行环境运行。沙箱临时目录还限制文件重命名，因此测试临时目录设为仓库内忽略的 `build/test-tmp`。测试未设置工具环境变量时显式跳过真实音频测试，不能据跳过结果声称音频链通过。

| 验证 | 结果 |
| --- | --- |
| 消息拆包、粘包、中文 UTF-8、超长/截断/重复长度头 | 通过 |
| 取消后的晚结果、取消后新调用、重复终态 | 通过 |
| 错协议、启动退出、截断响应、卡住握手 | 均被拒绝并有界回收；本轮故障用例分别约 0.06–0.52 秒 |
| 两个同内容文件、中文文件名、分页、Worker 重启、根内改名 | 独立来源 ID；确定改名保留原 externalId |
| 资源跨用户/实例、版本变化、撤销、遍历逃逸 | 拒绝 |
| Windows 根外符号链接 | 提升执行环境后测试通过；普通受限环境因创建符号链接权限不足会跳过 |
| 授权根被同路径空目录替换 | 身份不符，失败，未报告成功空库 |
| Provider 退出后已打开的独立源句柄 | 仍可读取；控制进程不承载正文 |
| ffprobe 经授权输入通道读取 4 秒 WAV | `pcm_s16le`，4000 ms，未知字段保留空值 |
| 伪装音频的 HLS 正文含远端分段地址 | ffprobe 与 FFmpeg 解码均拒绝；远端 HTTP 计数均为 0 |
| -6.020599913 dB Go gain，经 MP3 再解码测 RMS | 比例 0.500000 |
| 曲目 1500 ms 非零起点、定位标记 | 输出音频起点延迟 23.062 ms；标记结束位置误差 22.979 ms |
| 30 秒输入的首块输出 | 最终验证轮约 115.819 ms，输出尚未全部完成；首块阻塞期间处理调用仍未完成 |
| 首次写输出即消费者断开 | 最终验证轮约 152.146 ms 返回失败并回收 |
| 输入 SHA-256 前后比较 | 不变 |
| 测试结束检查 ffmpeg / ffprobe 进程 | 未发现遗留进程 |

时间测量是本机一次验证轮的观察值，不作为跨机器性能承诺。尚未测量独立的 CPU 百分比和整个处理进程树峰值内存。

## 4. 时间映射与资源限制

当前 `timeRestart` 在本 profile 的验证样本上采用 **100 ms 容忍范围**。`positionOriginMs` 表示请求的曲目起点；MP3 编解码延迟带来约 23 ms 的实测偏差，尚未通过 gapless 元数据精确消除。浏览器联合验收应沿用 100 ms 界限，不应声称采样级精准定位。未验证的格式、声道布局或更复杂 DSP 不能据此获得同样的定位能力。

固定 PCM 每块最多 4096 帧，复用 32 KiB 字节缓冲和 32 KiB 样本缓冲。子进程管道、HTTP 流和同步写入向上游传播背压，不把全曲 PCM 或输出先保存在内存后播放。任务输入需同时支持 `io.ReadSeeker` 和 `io.ReaderAt`；每个 Range 请求建立独立 `io.SectionReader`，避免探测并发读取导致共享 Seek 锁死。

探测与解码同时限定 `http,tcp` 输入协议及 `wav,mp3,flac,aiff,ogg,mov,matroska,webm,aac` 音频 demuxer，排除 HLS/concat 等可引用额外来源的播放列表。只限定网络协议不能防止音频文件伪装为播放列表，因此包含真实远端计数回归测试。

默认最多两个活跃音频 Worker，无无界排队；配置允许 1–8。任务最长 30 分钟。控制 RPC 最多 16 个在途请求，单消息最多 1 MiB；扫描最多 50,000 个候选，单页最多 200 条；分页快照最多 16 份、保留 5 分钟，单页扫描不占快照槽位。资源引用最多 4096 个并具备期限。Worker stdout 只用于协议；stderr 不传播私密路径或输入能力地址。

Windows 音频 Worker 置于 `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE` Job Object，取消通过控制上下文和关闭管道传播；回收 Job 时终止子进程树。Linux/Darwin代码采用进程组回收。基线当时仅验证Linux控制Worker故障回收，尚未验证Linux音频子进程；后续真实音频断连回收见第6节，Darwin仍未运行验收。

### Linux 原生验证（基线历史）

Arch WSL2，内核 `6.18.33.2-microsoft-standard-WSL2`，x86_64。测试目录为 `/mnt/d/Code/MusicFS/server`，文件系统为 WSL 挂载的 Windows 磁盘，不能据此声称 ext4/NAS 已验证。

Go 使用[官方 1.26.9 下载归档及校验清单](https://go.dev/dl/?mode=json&include=all)，Linux amd64 归档 SHA-256 为 `42d158b4d8f7b61ac0a830567c940a86098fb7aac52e467a5ebec03ef5cc2f8d`。验证完成后，整个临时环境移至 `build/_linux-validation`：工具为其 `_tools/go`，独立构建缓存、模块缓存和临时文件分别为其 `go-cache`、`mod-cache` 和 `tmp`。最外层下划线目录避免工具和活跃临时 Go 源文件被 `go test ./...` 当作仓库包扫描；均被 Git 忽略，未安装系统包或修改全局环境。

已实际运行并通过 `go test -count=1 -v ./tests/unit/plugins ./tests/contract/plugins ./tests/integration/plugins` 中的协议、DSP、来源与 Local Files 测试；真实 Worker 重启/移动身份和故障回收都通过。随后 `go test ./...` 全部通过，覆盖 SQLite、HTTP 契约、播放与端到端测试；`go test -race ./tests/unit/plugins ./tests/contract/plugins ./tests/integration/plugins` 也通过。Linux 未配置 FFmpeg，因此四个真实音频/播放列表隔离用例明确跳过；这部分通过证据仍来自 Windows 实际运行。

在最初基线阶段，恢复当时固定依赖 `golang.org/x/sys v0.34.0` 与 `golang.org/x/text v0.14.0` 后，再以 `-mod=readonly` 定向运行 `tests/unit/bootstrap`、`tests/contract/api`、`tests/integration/storage`、`tests/integration/playback`、`tests/e2e`，全部通过，未允许测试命令修改依赖清单。

该轮WSL DNS无法解析下载域名，改从Windows下载Linux FFmpeg仍过慢，已停止下载任务，未使用不完整归档测试。当时Linux真实FFmpeg流式处理、音频回收、定位及播放列表隔离未验收；后续结果见第6节。

## 5. 仍需明确的边界

- 官方 Worker 为管理员信任程序。进程独立、引用鉴权及 `os.Root` 不等于操作系统文件/网络沙箱。
- 来源版本以文件身份、长度与修改时间构成观测线索，不是已验证内容哈希或不可变快照。播放模块创建私有固定副本并再校验后，才能跨 Range 请求保持同一字节版本。
- 根内有符号链接的扫描保守报告不完整，不能用本轮评估缺失；真实 NAS 失挂、Linux 挂载置换、Windows junction 置换竞争仍需目标环境验证。
- 当前不自动无限重启 Worker。故障请求失败，由管理员或服务重启恢复；更完整的冷却熔断与增量断点续扫仍属后续增强。
- 开放第三方插件、在线 Provider、指纹、刮削和写回均未在本次 P4 范围实现。
- 独立音频集成测试不能证明某一浏览器能发声；实际浏览器格式探测、播放、快速连续定位、切歌和首声时间须在联合验收中单独记录。

## 6. 2026-10-10 Linux真实音频补验

Arch WSL原生FFmpeg/ffprobe 6.1.6采用官方签名源码本地构建，许可证为LGPLv2.1+，工具不随仓库分发；签名、构建与测量细节见[Linux音频记录](configuration-linux-audio.md)。使用已提交的依赖快照：yaml/v3 3.0.4、x/sys 0.48.0、x/text 0.42.0；工作区用户后续升级不属于本轮结果。

| 定向检查 | 实测结果 |
| --- | --- |
| YAML处理、PCM定位与重启 | 20.92秒通过；完整输出6024ms、定位输出2040ms、起点23.333ms；220/440/660Hz三段标记正确，同YAML重启后行为保持 |
| YAML音频启用边界 | 20.25秒通过；以上两项E2E包共41.185秒 |
| 真实音频及伪装播放列表隔离 | 包测试44.075秒通过；实际Go gain的RMS比例0.500000，定位起点23.062ms、误差22.979ms；断连回收79.21ms，首块输出94.70ms |

毫秒值仅为本机本轮观察，不作跨设备性能承诺。后续Linux相关包完整回归与E2E/配置读写race均通过，NCM固定摘要测试的平台修复及Windows/Linux结果见[Linux音频记录](configuration-linux-audio.md)。上述结果不代表Linux浏览器、人工听音、ARM/macOS或NAS及所有文件系统已验收，也不包含动态插件启停。
