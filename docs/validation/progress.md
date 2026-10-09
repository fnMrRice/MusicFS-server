# 最小播放基线实施记录

日期：2026-10-09。按用户要求逐步实现并分别提交，在最简 Web 完成后停止。本次交付范围为 P0—P4 的本地播放基线及对应 P8 自动验收；P5—P7、桌面 Client 和新增 YAML/完整插件管理计划不在本次交付内。分章保留完整目标，下面的证据不代表各章所有扩展要求均已完成。

第 1—3 节保留最简 Web 基线阶段的历史范围和验收事实；此后用户授权的独立 NCM 插件增量记录在第 4 节，不回写旧测试数量、制品数量或当时依赖版本。

启动步骤见[运行说明](../running.md)，依赖见[版本记录](../dependencies.md)，插件实测见[插件验证](plugins.md)，真实浏览器证据见 [Web 兼容记录](../../../web/docs/compatibility.md)。各仓库独立提交，未签名、未推送。

## 1. 逐步实现与提交

提交标题保留任务编号；`git log --oneline --all --grep=<编号>` 可查首次实现与后续修复。表内短摘要为本 Server 仓库提交，Web 步骤在其独立仓库记录。

| 步骤 | 已实现及验证范围 | 主要提交 |
| --- | --- | --- |
| S-P0-01 | Go 1.26.9、单 module、依赖固定；Windows/Linux 构建 | `29a788d` |
| S-P0-02 | src/app 薄入口与按职责分模块 | `16bd53f` |
| S-P0-03 | 根 tests 布局、通过公开包接口测试 | `2411bea` |
| S-P0-04 | 构建、格式、静态检查、覆盖率与竞争检查入口 | `cb0b5ef` |
| S-P0-05 | OpenAPI、JSON Schema、插件模型及成功/失败契约测试 | `c1d1d13`、`e53b38e` |
| S-P0-06 | 可再生成的合成音频夹具与来源说明 | `a68664c` |
| S-P1-01 | 严格 JSON 配置、相对路径、目录分离、受控监听；新增 YAML 要求待迁移 | `1884430` |
| S-P1-02 | 单数据目录锁、启动就绪、信号取消与有界退出 | `a89c61f` |
| S-P1-03 | HTTP 路由、请求限制、统一错误与控制面期限 | `ce52b77` |
| S-P1-04 | 管理凭据交换 Cookie、CSRF、Origin、限流、持久会话和撤销 | `ba1d052` |
| S-P1-05 | SQLite WAL、事务迁移、升级前备份、新版 schema 拒绝 | `00dcc1b`、`94ad6ca` |
| S-P1-06 | 持久任务、去重/幂等、单次领取、重启中断处理与安全错误 | `e0d2a78`、`7c478c8` |
| S-P2-01 | 真 Local Files 子进程、握手、取消、退出和故障路径 | `524e05b`、`b9e2d4b` |
| S-P2-02 | os.Root、限定资源引用、文件身份及版本校验 | `06125aa`、`a4ab3bf` |
| S-P2-03 | 稳定来源 ID、完整扫描暂存后发布；离线/替换根不清库 | `037ba2e` |
| S-P2-04 | ffprobe 受限输入元数据、未知值保留 null、实际工具能力 | `6d08249` |
| S-P2-05 | 来源/曲目查询、固定修订游标与管理扫描入口 | `6cb804f`、`96d6c11` |
| S-P3-01 | 分离 session/attempt/representation，生成私有固定输入副本 | `d498b95` |
| S-P3-02 | 有限会话、空闲/总期限、撤销、停机与重启清理测试 | `94dbf13` |
| S-P3-03 | 精确登录会话授权、独立偏移、流取消与写超时 | `9846576` |
| S-P3-04 | 原流 HEAD/Range/ETag/If-Range、400/206/416 与真实 HTTP 验证 | `3f45217`、`4f8e71f` |
| S-P3-05 | attempt 并发代次、持久幂等、有限 seek/recover、停止 | `9ca2c5a` |
| S-P3-06 | 服务组合、同源静态 Web 与全新目录端到端启动 | `b9f8403` |
| S-P4-01 | 实际工具/profile 探测；限定 demuxer 防外部播放列表引用 | `6d08249`、`a0e95e4` |
| S-P4-02 | 任务限定二进制输入/输出通道、独立音频 Worker | `c70eec9` |
| S-P4-03 / 04 | 有界 PCM 数据契约、可替换 Go gain 节点与解码/编码管线 | `ea7d715`、`d857f2f` |
| S-P4-05 | timeRestart 新尝试、新 URL、串行定位与撤销期间拒绝创建 | `9ca2c5a`、`7ef6b1a` |
| S-P4-06 | 真实增益、位置标记、首块时序；另有 Edge 联合验收 | `997d0a3` |
| S-P4-07 | 处理并发、背压、任务期限及 Windows 进程树回收 | `c70eec9`、`79bcfa7` |
| S-P4-08 | 输入固定副本、表示版本及权限隔离；处理输出不持久缓存 | `d498b95`、`9846576`、`9ca2c5a` |
| PL 协议/宿主 | 版本化帧、能力、受控资源、正式 Worker、可选管理员固定 SHA-256 | `2d60cfe`、`d624eb8`、`bcb54cd`、`b4844a9`、`3da8bee` |
| P8 本次子集 | 可重复检查、三程序双平台构建、运行说明、真实浏览器矩阵与限制 | `4ee2175`，其余收尾提交按 S-P8 编号检索 |

## 2. 实际执行结果

| 环境/检查 | 结果与边界 |
| --- | --- |
| Windows amd64，Go 1.26.9 | gofmt、go vet、完整 Server 单元/契约/集成/端到端测试通过；设置真实 FFmpeg/ffprobe 路径 |
| Windows 并发竞争检查 | MSYS2 UCRT64 GCC 配合 CGO，`go test -race ./tests/...` 通过 |
| Windows 全新服务流程 | 实际扫描脚本、登录、列表、Range、跨会话拒绝、撤销、重启与退出通过 |
| 构建产物 | Windows amd64、Linux amd64 各生成 musicfs、musicfs-plugin-local、musicfs-plugin-audio-ffmpeg；输出在忽略的 build 目录 |
| Linux amd64 原生测试 | Arch WSL2、Go 1.26.9：`go test ./...` 通过；插件竞争检查通过；FFmpeg 缺失，四项真实音频用例明确跳过 |
| Linux 文件系统 | 测试使用 /mnt/d 的 Windows 磁盘挂载，不能推定 ext4 或 NAS 已验证 |
| 真实 Windows 音频 | Go gain -6.0206 dB 后 RMS 比例 0.5；定位误差约 23 ms；30 秒输入首块约 116 ms 且尚未处理完成；断连约 152 ms 回收 |
| Web 自动检查 | 23 项单元测试、6 项完整 Edge E2E 及定向键盘/滚动验证通过；生产构建通过 |
| Edge 154.0.4258.62 | 真实 WAV 解码非零 PCM、暂停/继续、原流定位、快速切歌/停止、鉴权恢复、撤销和分页通过 |
| Edge 处理播放 | AIFF 直放失败后，服务端解码→Go DSP→MP3 可播放；连续 2 秒→4 秒定位，FFT 标记符合目标音段 |
| Edge 600 秒 AIFF | 处理流响应约 468 ms，非零解码 PCM 约 1374 ms，均发生在输出流完成前；这是本机单轮观察值 |

最终覆盖率报告中 src 总体语句覆盖为 70.2%；覆盖率不代替真实浏览器/音频证据。检查脚本与构建使用 `-mod=readonly`，防止验证时静默改变依赖。真实工具测试未配置工具路径时会跳过，不能用跳过结果声称处理链通过。Windows 受限执行环境阻止本机音频通道时，真实集成验证在允许本机网络的执行环境完成。

浏览器截图与结构化观察值位于 Web 的 `docs/validation`；浏览器实际解码非零 PCM 已验证，但未进行人工扬声器听音。

## 3. 明确保留的边界

- 当前配置为 JSON；YAML、插件/实例管理开关、平滑停用租约是新计划，尚未实现。官方 Worker 受管理员信任，不等于操作系统沙箱。
- 扫描增量请求目前执行受控完整复核；没有文件监视、自动重试调度、全面排除规则或自动熔断恢复。扫描异常不发布成功空库。
- “可用”只代表当前来源可读取，不能保证任意文件可解码。仅声明实测 WAV/MP3 原流与 MP3 处理 profile；其他浏览器、格式、ARM 和 macOS 未验收。
- 输入在创建会话时复制到有配额的私有磁盘，保持不可变字节；不把整曲装入内存。当前每次处理均流式生成，不缓存处理输出，因此没有完成持久转码缓存、跨会话复用和缓存发布系统。
- timeRestart 采用 100 ms 验收容忍范围；未承诺采样级定位或所有格式等同精度。处理流没有伪造 Content-Length、ETag 或原文件字节 Range。
- Linux 真实 FFmpeg、子进程树回收、物理 NAS 失挂/重挂、junction 竞争、ext4 与人工听音仍待目标环境确认。
- 备份/迁移已有测试；完整灾难恢复演练、Docker、桌面管理租约及所有 P8 部署要求仍未完成。
- 在线 Provider、识别、刮削/写回、歌单、事件推送、第三方插件与 Client 均不随最简 Web 宣称交付。

本轮到此停止功能扩展。后续按分章中的剩余门槛继续，不将本记录或截图当成全部长期架构的完成证明。

## 4. 后续授权增量：独立 NCM 插件

日期：2026-10-09。已实现 Go 标准库解密核心、`musicfs-plugin-ncm` 独立 Worker、任务限定 Range 通道和 Server/Web 接入。构建入口现包含 Server、Local Files、音频处理、NCM 四个程序。配置与手工步骤见 [NCM 说明](../ncm.md)和[运行说明](../running.md)。使用已记录的 MIT 项目作为格式参考，没有新增第三方解密运行依赖。

| 检查 | 已完成的结果与边界 |
| --- | --- |
| 核心单元测试 | MP3/FLAC 载荷逐字节、随机/并发读取、元数据复制、可选标签、封面填充、EOF、畸形及截断输入通过；`go test -mod=readonly -race ./tests/unit/ncmdecoder` 通过（2.199 秒） |
| 模糊测试 | `FuzzMalformedNCM` 单轮 `-fuzztime 10s -parallel 2` 执行 299,534 次，未发现崩溃；不表示穷尽格式变体 |
| Windows 真实 Worker | `go test ./tests/integration/plugins -run '^TestActualNCM' -count=1` 通过（9.1 秒）；随机 Range、限额、取消回收、损坏输入和哈希拒绝通过 |
| 最终通道检查 | 追加重复 Content-Range 拒绝后，`TestActualNCMChannelValidation` 重跑通过（7.9 秒）；错误 URL/nonce/task、重定向、错误状态/长度/范围、短正文、压缩和重复 open 均拒绝 |
| 契约与静态检查 | NCM wire 有效/无效样本、既有 HTTP/API 契约及 NCM 包/入口 vet 通过；公共 HTTP effectiveProcessing 沿用可扩展类型 |
| Server 端到端 | `TestNCMServerRoundTripAndDisabledPlugin` 启用/未启用两个场景通过；覆盖 Range、权限、幂等、不可变表示、源版本变化、撤销和错误后普通 WAV 可播放。另验证 2 MiB 封面使容器超预算但解密载荷可缓存，音频输出超预算返回 429；最终定向测试 3.656 秒通过 |
| 独立 Node 样本 | Web 自行封装项目 CC0 MP3/FLAC，不调用 Go fixture builder；Go 核心逐字节还原 MP3 33,432 字节、FLAC 87,122 字节 |
| Edge NCM 三项联调 | Edge 154.0.4258.62，最终三项通过（19.3 秒）：MP3 解密/Range/非零 PCM、FLAC 转 MP3/4 秒位置标记、损坏 NCM 错误与普通 WAV 恢复 |
| Linux 真实 Worker | Arch WSL2 / Go 1.26.9 原生测试通过（8.622 秒）；这项验证无需 FFmpeg，不替代 Linux 真实 FFmpeg 验收 |
| 本轮全量回归与 race | `scripts/check.ps1 -Race` 全部通过：gofmt、vet、覆盖率测试和 race；配置真实 FFmpeg/ffprobe 6.1.1，未跳过该音频集成。使用升级后的 go.mod/go.sum；第 2 节仍为增量前的历史证据 |
| 四程序双平台构建 | `scripts/build.ps1 -TargetOS windows` 与 `-TargetOS linux` 均通过，分别生成 Server 与三个插件的 amd64 制品；Linux 交叉构建与上面的原生 Worker 运行结果分别记录 |

浏览器详情、截图和结构化证据见 [Web NCM 验证](../../../web/docs/ncm-validation.md)。本轮还通过 Web 生产构建、类型检查和既有 23 项单测；三项 NCM E2E 与此前完整 E2E 分开记录。

SourceAsset 仍代表原始加密文件；纯解密表示保持内部音频字节且独立确定长度/ETag/Range，转码输出继续遵守处理流语义。`effectiveProcessing` 增加 `ncmDecrypt`，没有新增 HTTP 端点或必填字段；原文件不改写，私有解密快照受既有缓存配额约束。

真实下载 NCM 样本、所有网易云客户端版本及其他浏览器尚未验收；非零 PCM/频率测量不能代替人工扬声器听音。当前结论限定于合成样本、独立 Node 封装、所列系统与实际测试路径。
