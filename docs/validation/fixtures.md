# S-P0-06 媒体夹具与播放验收基线

夹具为项目生成的数学正弦波，CC0-1.0；没有私人音乐、第三方录音或真实凭据。输入摘要、长度、探测结果和生成工具版本见 `tests/testdata/audio/manifest.json`。浏览器和 OS 支持必须在 P3/P4 中填实测结果；本步骤只验证文件合法性与可测标记。

## 复现与内容

```text
python scripts/generate-fixtures.py --ffmpeg <ffmpeg-executable> --ffprobe <ffprobe-executable>
go test ./tests/contract/api
```

生成工具为 Python 标准库加 FFmpeg 6.1.1 full shared（构建带 GPL/version3，未把工具二进制纳入仓库）。WAV 采用 48 kHz、双声道、16-bit little-endian PCM；峰值为 0.125，即约 -18.06 dBFS，正弦稳态 RMS 约 -21.07 dBFS。每个边界 10 ms 平滑处理，试听前仍应调低设备音量。

| 文件 | 内容与验收用途 |
| --- | --- |
| markers.wav | 6 秒无音乐标签；0–2 秒左声道 220 Hz，2–4 秒右声道 440 Hz，4–6 秒双声道 660 Hz |
| 中文 空格曲目.wav | 6 秒；声道时间同上，频率分别 330/660/990 Hz，用于中文路径、空格、切歌辨识 |
| silence-prefix.wav | 8 秒；前 2 秒数字静音，之后与 markers.wav 一致，用于非零位置和静音前缀 |
| processing.aiff | 合法 24-bit big-endian AIFF；用于处理路径候选，不能未经浏览器实测就声称一定不可直放 |
| lossless.flac | 6 秒 FLAC 无损压缩，验证内容探测与无标签输入 |
| vbr-untagged.mp3 | 有前导静音的 libmp3lame VBR q4；无 ID3 音乐标签；manifest 记录多种实际帧长度，不能把平均码率当 CBR |
| corrupt.wav | 错误 RIFF 长度和非法 chunk 内容，必须拒绝作为合法可播 PCM |
| empty.wav | 零字节输入，不能返回伪造时长或成功播放计划 |

编码格式、采样率和声道由 FFprobe 对合法夹具验证。SHA-512 每次测试核对，WAV 测试额外核对静音、实际频率、声道与峰值。生成结果的身份依赖内容摘要；换编码器版本应重新生成并复核，不能假设有损输出跨版本逐字节相同。

## P3/P4 验收填写模板

以下每个条目都应填写“通过/失败/未执行”、证据路径和限制；未填视为未执行。

| 项目 | 实测记录 |
| --- | --- |
| 日期、提交、Server OS/架构、Go/FFmpeg 精确版本 | 待实测 |
| 浏览器名称/完整版本、客户端 OS、真实音频输出设备 | 待实测 |
| 输入文件名、SHA-512、探测格式、输出 profile MIME/codec/container/transport | 待实测 |
| 已听到正确曲目、左右声道、时间段标记和音量 | 待人工试听；事件/波形测试不能代替此项 |
| 创建请求至首个音频帧/可闻输出时间 | 待实测 |
| P3 暂停/恢复、中段 Range、尾部、连续 A/B/A 切歌 | 待实测 |
| P4 全曲未处理结束前开始播放、gain PCM 数值变化 | 待实测 |
| P4 timeRestart 起点、允许误差和实际误差、前导静音定位 | 待实测 |
| 快速定位、响应丢失同键重试、定位中切歌/停止 | 待实测 |
| Cookie、另一个用户/会话、撤销和过期、重启旧 attempt | 待实测 |
| 停止/断连后请求、文件引用、处理子进程和缓存回收 | 待实测 |
| 输入 SHA-512 前后相同；资源/内存没有持续增长 | 待实测 |

P0 不发布 codec 支持矩阵。首个目标为 Windows amd64 + 实际浏览器；Linux Server 运行与其他浏览器、ARM64 分别验证。`canPlayType`、扩展名、HTTP 200、时间增长或 FFprobe 成功均不足以宣称浏览器有声播放成功。
