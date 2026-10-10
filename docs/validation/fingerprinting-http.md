# S-P5-03 指纹配置与 HTTP 联合验证

本记录只说明本轮配置、接口和 Windows 真实服务链路，不代替核心持久化、恢复、匹配候选和双平台最终验收。使用当前仓库固定的 Go 1.26.9、JSON Schema 6.0.3、YAML 3.0.5、x/sys 0.49.0、SQLite 1.60.1，未变更依赖。

## 实现边界

`fingerprinting` 默认关闭。显式启用须提供 fpcalc 路径与 SHA-256，并启用官方 FFmpeg 音频实例；并发 1—2，默认1，总任务期限 1—120 秒，默认30秒。总期限包含 NCM 解密、规范化、提取和发布；失败收尾有独立有界期限。内部规范化另有30秒期限。后台规范化和 NCM 解密各用独立1槽位，不消耗前台播放的准入槽位。

本轮提供创建、查询、自然幂等取消、当前资产索引分页和按所有者读取明细五个端点。固定 Chromaprint 1.6.1、algorithm 2、DSP 前 11025 Hz mono s16le；请求起点 0—24 小时、时长10—120秒。没有自动指纹扫描、外部检索、上传或 Web 指纹管理页面。算法/工具身份不可由 HTTP 请求覆盖。详情正文有界并独立保存于 MediaStore；响应没有私有路径或内部能力票据。

## 已完成的定向检查

- 配置包通过，4.169 秒：缺省与部分覆盖、严格类型/未知字段、边界、必需路径与摘要、相对路径和摘要大小写归一、音频插件/实例双层启用；加载器与编辑器 Schema 有效性一致。
- HTTP 契约包通过，3.448 秒：全部模型/样本/OpenAPI 引用；真实 SQLite 与 Cookie/Origin/CSRF；范围与请求字段；幂等重放、范围冲突、队列合并、自然幂等取消；其他所有者的任务/索引/实际已发布明细均不可读，明细所有者可以读取；禁用端点返回 `UNSUPPORTED`。
- 插件契约包通过，0.423 秒：`audio.normalize` 请求、能力声明、固定 PCM 描述和范围；不接受 DSP 参数或错误输出格式。
- Windows 真实 YAML 服务测试通过，10.49 秒（测试包10.567秒）：构建 Local Files、FFmpeg 与 NCM Worker，加载正式配置，登录、扫描、提交真实任务并校验实际媒体文件、接口明细和播放。

HTTP 控制测试的队列取消使用停止的提取 worker，保证取消与合并断言确定；私有明细采用有效的合成指纹直接发布。真实工具另由下述完整服务测试覆盖，不能把控制测试当作实测解码。

## 真实工具与音频证据

Windows 使用 FFmpeg/ffprobe 6.1.1，fpcalc 1.6.1 Windows x86_64 的 SHA-256 为 `00dcc56d911f2dea84737aa9dc8e2d118c9eb7a037d815d1ed001d8593e8fbee`。工具不随本仓库分发。所有输入为测试代码合成的32秒变化音调，派生 MP3、FLAC 和独立 NCM 容器；没有下载或读取私人音乐。

| 原始资产 | 实际规范化样本 | 指纹值数量 | 质量 |
| --- | ---: | ---: | --- |
| MP3 | 353241 | 237 | available |
| FLAC | 352800 | 237 | available |
| NCM（内层 MP3） | 353241 | 237 | available |

MP3 和 NCM 结果保留解码器实际尾部样本，不强行改成 FLAC 样本数。FLAC 起点7毫秒对应第77个样本，10000毫秒请求得到110250个样本；从30秒请求10秒时只记录实际22050个样本并标记 `short`，没有补零扩大覆盖。

测试复核媒体文件实际长度与 SHA-256、明细与索引一致性、明细重新校验、匿名401、每页1条的后续分页、即时取消和重复取消。删除原 FLAC 后仍能读取3条持久索引和原明细。启用、禁用、故意错误工具摘要三种配置均通过 WAV Range 与停止回归；后两种配置的指纹请求为415 `UNSUPPORTED`。

首次真实服务测试因接口 Schema 的媒体 MIME 与实现 `application/json` 不一致而失败，已修正契约并重跑通过。新增私有明细测试首次在默认 Windows 沙箱临时目录发布时被拒绝 rename；改用工作区临时目录并在允许本地子进程/原子文件操作的测试环境运行后整包通过，未放宽生产文件校验。

## 复验与范围

设置 `MUSICFS_FFMPEG`、`MUSICFS_FFPROBE`、`MUSICFS_FPCALC` 指向本机已校验工具后，运行：

```powershell
go test -mod=readonly ./tests/unit/config ./tests/contract/api ./tests/contract/plugins
go test -mod=readonly ./tests/e2e -run '^TestYAMLFingerprintsActualToolsMediaAuthorizationAndPlayback$' -count=1 -v
```

Windows 文件发布环境须允许原子 rename/write-through；受限环境可以把 `TEMP/TMP` 指向工作区独立临时目录。未设置真实工具时该 E2E 明确跳过，不能记作通过。全平台、race、总期限回归和其他核心测试由本阶段最终联合验收补充；本记录不宣称 Linux/macOS/ARM/NAS 或浏览器指纹界面已验收。自动化样本验证也不代替人工听音或跨来源录音匹配准确率评估。
