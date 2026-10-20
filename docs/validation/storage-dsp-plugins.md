# 存储与 DSP 插件验证

日期：2026-10-20。验证平台为 Windows amd64，真实音频使用 FFmpeg/ffprobe 6.1.1。验证时的代码尚未提交或发布，本记录只描述 DSP/存储增量快照的实际验证，不沿用历史版本的 Linux 或其他平台结论。

## 已验证行为

| 范围 | 实际证据 |
| --- | --- |
| SQLite 必需组件 | 发布配置默认使用独立 SQLite Worker，包含程序实际 SHA-256；缺少该程序不能打包。`storage.enabled: false` 被配置校验拒绝；存储不进入普通插件启停列表，禁用 `storage`、`sqlite` 或 `musicfs.plugin.storage.sqlite` 均返回 `404 NOT_FOUND`，随后数据库仍可读取。 |
| 完整存储边界 | 真实子进程覆盖主库与实例私有状态、CAS 冲突、跨实例隔离、内部字段往返、50,000 项扫描事务、嵌套文件保护回调、取消与后续调用、未知提交后的不自动重试，以及程序摘要和契约不匹配时拒绝打开数据库。 |
| 传输保真 | 空集合、nil、零值指针和公开 JSON 隐藏字段保持不变；错误码、取消、记录不存在和显式任务重试标记保留。回调只允许执行一次，结束或取消后失效。 |
| DSP 真实处理 | 内建 -3 dB 与独立 gain Worker -6 dB 的组合输出通过 RMS 验证；覆盖强制处理流、执行顺序、缓存身份分离、seek 保留原链、相同链跨源恢复、不同链要求重启播放，以及指纹规范化 PCM 字节不变。 |
| 协议及故障 | DSP 拒绝错误摘要、身份、版本、非有限 PCM、篡改时标、有状态算法声明、非零延迟和超限帧；真实进程覆盖超时、崩溃、取消与回收。新版宿主拒绝旧 `0.1.0` 音频 Worker，避免忽略新增处理链。 |
| 完整主服务 | `TestExternalStorageAndDSPServerRestartAndPortableRestore` 使用独立 SQLite 和 DSP 程序，验证扫描、处理播放、管理设置持久化、重启、启动前私有状态只读加载及管理快照脱敏。 |
| 运行时就绪状态 | 真实存储进程启动后可用，取消启动上下文不影响运行；关闭或提交后崩溃立即失去可用状态。完整启动装配的 HTTP 测试确认存储不可用时 ready 返回 503，live 仍返回 200，既有初始化和排空判断保持有效。 |
| 备份与恢复 | 外部 SQLite 生成便携备份；移除原插件程序后仍可静态校验并恢复到内置 SQLite，恢复配置清空外部 DSP。主服务组合测试及 backup、mediastore 集成包均通过。 |
| 配置一致性 | 存储、数据根或 DSP 部署参数变化要求重启；配置重载在读取新数据库或执行新程序之前拒绝变化。管理快照不输出启动令牌、存储选项或外部程序配置。 |

## 复验入口

完整回归通过：所有源码、契约、单元、集成、E2E 和发布脚本测试包均通过，E2E 包耗时 299.728 秒。之后的就绪检测、显式空参数配置、传输深度限制和容器配置改动分别补跑受影响测试。最终静态检查通过，Linux amd64 的全部源码交叉编译通过；交叉编译不等于 Linux 运行验收。复验命令：

```text
go test -mod=readonly -timeout=15m ./src/... ./tests/... ./scripts/release ./scripts/generate-storage-rpc
go vet -mod=readonly ./src/... ./tests/... ./scripts/release ./scripts/generate-storage-rpc
```

上述检查对应本次 DSP/存储增量。工作区中的独立 SDK 模块拆分不属于本记录的验证范围；未来合并该拆分后须重新检查。

设置 `MUSICFS_FFMPEG` 和 `MUSICFS_FFPROBE` 为本机真实程序路径后，可复验实际音频与主服务组合：

```text
go test -mod=readonly ./tests/integration/playback -run TestDSPActualPlaybackCacheSeekRecoveryAndFingerprintIsolation -count=1
go test -mod=readonly ./tests/e2e -run TestExternalStorageAndDSPServerRestartAndPortableRestore -count=1
```

默认测试会跳过需要外部音频工具、真实平台账户或安装制品的用例，不能把跳过计为通过。Windows 文件锁、目录联接和原子替换需要测试进程具备临时目录的正常系统权限；沙箱拒绝这些操作的结果不计为功能验收。

## Windows 安装制品

最终验证包位于忽略目录 `build/releases/final-sqlite-dsp-validation-e62ca6301a0e4b549d259c8541ae1679`，包含 Server、统一普通插件程序、必需 SQLite 插件、可选 DSP gain 示例和工作区已有 Web 制品。标记为 `dirty: true`，验证时未提交、上传或公开发布。

使用不依赖当前 Server/SDK 源码模块的独立检查器直接运行该安装包，全部通过，总耗时 4.104 秒：

- 全部 60 项文件 SHA-256 匹配；默认配置指向包内独立 SQLite 程序，摘要一致，清单标记为必需。
- 复制到独立临时目录后启动、健康检查和鉴权成功，实际扫描合成 WAV/FLAC 文件。
- WAV 精确 Range 校验通过；FLAC 处理流解码得到 6000 ms PCM，定位到 4 秒后的输出为 2000 ms，标记频率为 660.000 Hz、RMS 为 0.118736。
- 包内现有 Web 的 HTML、JS、CSS 通过 HTTP 取回；这不等于浏览器交互验收。
- 原音频字节保持不变，释放最后一个客户端租约后 Server 正常退出。

此检查没有重编或替换包内二进制，也没有扩展 NCM 验收；它补齐安装制品证据，不替代并行 SDK 拆分完成后的源码回归。

## 范围限制

- DSP v1 仅支持固定 48 kHz 双声道 float32、无状态、保持帧数与时标、无延迟和尾音的节点；有状态均衡、混响、重采样等不在此版范围。
- 持久化 v1 使用匹配版本的 Go SDK 和完整 Repository 契约，便携备份格式为 SQLite；尚未提供 PostgreSQL、MySQL 或跨语言 SDK。
- 本次未验证 Linux/macOS 实际运行、任意第三方二进制、生产规模吞吐及长时间压力。进程隔离和摘要校验不等于恶意代码沙箱。
- Web 现有处理节点解析可兼容新增可选字段；桌面客户端尚无对应实现可做运行验收。
