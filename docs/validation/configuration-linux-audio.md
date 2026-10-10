# YAML 配置的 Linux 真实音频验收

日期：2026-10-10。补齐[配置验收](configuration.md)中未执行的 Linux FFmpeg 路径，验证新版 YAML 启动、处理后播放、定位与同配置重启。本轮不实现旧配置兼容或动态插件启停。

## 环境与可复现入口

- Arch WSL2 / Linux amd64，内核 `6.18.33.2-microsoft-standard-WSL2`，Go 1.26.9。
- 仓库位于 WSL 的 Windows 磁盘挂载；测试的服务数据、配置与音乐使用原生 Linux `/tmp` 下的独立目录。不能据此推断其他 Linux 文件系统或 NAS 已验证。
- 使用配置迁移提交时的 `go.mod/go.sum` 快照，通过 `GOFLAGS=-mod=readonly -modfile=...` 同时约束测试及其子进程构建：yaml/v3 3.0.4、x/sys 0.48.0、x/text 0.42.0。工作区另有用户更新的依赖，本记录不将其视为本轮固定环境。
- FFmpeg/ffprobe 6.1.6，官方签名源码以 GCC 16 本地构建，启用 libmp3lame。构建输出为 LGPL 2.1 or later；工具、源码、缓存与日志放在忽略的 `build/_linux-validation`，不随仓库分发，未安装系统包。

在 Linux 仓库根设置实际工具绝对路径后，可重复运行以下检查；工具缺失造成的跳过不算通过：

```sh
export MUSICFS_FFMPEG=/absolute/path/to/ffmpeg
export MUSICFS_FFPROBE=/absolute/path/to/ffprobe
export GOFLAGS=-mod=readonly
export CGO_ENABLED=1
go vet ./src/... ./tests/...
go test -count=1 -v ./tests/e2e ./tests/integration/plugins \
  -run 'Test(YAMLAudio|ActualAudio|AudioInputs|ActualNCMWorker)'
go test -count=1 ./tests/e2e ./tests/integration/plugins \
  ./tests/integration/config ./tests/unit/config ./tests/unit/bootstrap
go test -race -count=1 ./tests/e2e ./tests/integration/config
```

测试自行构建插件、生成临时 YAML、分配端口和创建独立数据库，不需要音乐账号或私人样本。Linux race 检查还需要本机 C 编译器。若使用自行裁剪的 FFmpeg，必须包含 MP3 编解码、FLAC 解码、WAV、PCM `f32le` 输入输出及重采样支持。用于隔离回归的工具另启用了 HLS/concat/mpegts，避免因工具本身缺少播放列表支持而掩盖输入限制错误。

## 真实输出

新增 `TestYAMLAudioPCMSeekAndRestart` 经 YAML 加载启动真实 Server 和插件，扫描项目 CC0 FLAC，再通过 HTTP 获取处理后的 MP3，并重新解码为 48 kHz 单声道 PCM。测试覆盖：

1. 元数据为 FLAC / 6000 ms；协商 MP3 `processed` / `timeRestart`。
2. 完整输出包含 220、440、660 Hz 三段已知标记，非零 RMS 且无非法样本。
3. 请求 4000 ms 定位后生成新 attempt/representation，保留会话与资产；旧地址返回 409，新输出为末段 660 Hz。
4. 使用同一 YAML 与数据目录重启，保留来源及资产身份，旧播放地址返回 404，新会话仍产生三段标记。
5. YAML 与源音频前后字节一致；这项重启检查针对新版自己的持久化，不涉及旧版迁移。

| 定向检查 | 实测结果 |
| --- | --- |
| YAML PCM、定位与重启 | 20.92 秒通过；完整输出 6024 ms，定位输出 2040 ms，660 Hz 起点 23.333 ms；时长/起点容忍范围 100 ms |
| YAML 插件/实例启用边界 | 20.25 秒通过；仅双层启用可处理，关闭任一层均不启用音频插件；两项 E2E 共 41.185 秒 |
| 真实音频增益与定位 | RMS 比例 0.500000；定位起点 23.062 ms，标记误差 22.979 ms |
| 断连与流式首块 | 断连后 79.212 ms 返回并清理；30 秒输入在 94.696 ms 产生首块，任务当时尚未完成 |
| 伪装播放列表 | 探测与解码均拒绝，外部测试 HTTP 计数为零；以上真实音频与隔离检查共 44.075 秒 |
| Windows 新 YAML 回归 | 同组两项 E2E 的 race 检查通过，包耗时 12.384 秒；完整与定位时长及频率标记与 Linux 一致 |
| NCM Worker 完整定向回归 | Linux 7.360 秒、Windows 7.744 秒通过；随机 Range、限额、取消/槽位回收、损坏输入和固定摘要拒绝均保留 |

NCM 摘要测试原来在执行过的程序上直接追加字节，本轮 Linux `/tmp` 连续三次出现 `ETXTBSY`；只运行未启动程序的摘要子项则通过。测试改为同目录创建修改副本后替换原路径，仍验证已固定摘要拒绝新程序，并加强关闭错误断言。没有通过重试、延时或删除取消检查掩盖错误，也没有修改 NCM 生产行为。

修复后的最终 Linux 回归中，`go vet ./src/... ./tests/...` 通过；五个相关测试包全项通过：E2E 85.901 秒、插件集成 66.860 秒、配置读写集成 0.156 秒、配置单测 0.144 秒、启动单测 0.010 秒。真实音频用例均执行，没有跳过。随后 E2E 与配置读写集成的 race 检查分别以 80.687 秒、2.136 秒通过。测试结束检查未发现本轮 Worker、FFmpeg 或 ffprobe 遗留进程。这些是相关包的完整回归，不冒充全部 Server 包重新运行的结果。

## 工具来源与摘要

源码来自 [FFmpeg 官方下载](https://ffmpeg.org/download.html)及其 [6.1.6 归档](https://ffmpeg.org/releases/ffmpeg-6.1.6.tar.xz)。按官方发布密钥核验分离签名，主密钥指纹为 `FCF986EA15E6E293A5644F10B4322F04D67658D8`；使用独立验证密钥目录，未修改用户密钥环。

| 本轮文件 | SHA-256 |
| --- | --- |
| 官方 `ffmpeg-6.1.6.tar.xz` | `d4fcb164028dd3beee5d92c0ac72e46aac6973c75ea12dc14de07bf8f407370a` |
| 本地构建 `ffmpeg` | `53a5cc8e7998859fdbe638b06f0f5d5127253fc7e3e67b62dba4dce32573e29b` |
| 本地构建 `ffprobe` | `43885c25dd785724748c38d95b979a4c44714f2f9cbb51aaa4762e6f8548fda0` |
| Arch `lame-4.0-1-x86_64.pkg.tar.zst` | `2c7bf7150e41a12b7b2ef62fec6ab5a071e20b537ac734490922fa64c204af77` |
| Arch `mpg123-1.33.7-1-x86_64.pkg.tar.zst` | `af9bbea4f7c003f2c4b604430582d58b7b232c5adcda673d15401c3faf1943f2` |

[LAME](https://archlinux.org/packages/extra/x86_64/lame/) 与 [mpg123](https://archlinux.org/packages/extra/x86_64/mpg123/) 的 Arch 包仅解压到测试工具目录；动态库通过本地构建的运行时查找路径加载。以上二进制摘要标识本机这次构建，不承诺跨目录可复现相同二进制。

## 结论边界

证据覆盖服务端真实解码、DSP、编码、HTTP 交付和配置重启行为。毫秒测量仅是本机单轮结果；Linux 浏览器、人工扬声器听音、ARM、macOS、物理 NAS、挂载置换和所有格式组合仍未验收。Windows 浏览器证据继续见[配置验收](configuration.md)；不以 PCM 测量代替听音。
