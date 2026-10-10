# 最小播放基线实施记录

日期：2026-10-09。按用户要求逐步实现并分别提交，在最简 Web 完成后停止。本次交付范围为 P0—P4 的本地播放基线及对应 P8 自动验收；P5—P7、桌面 Client 和新增 YAML/完整插件管理计划不在本次交付内。分章保留完整目标，下面的证据不代表各章所有扩展要求均已完成。

第1—3节保留最简Web基线阶段的历史范围和验收事实；此后独立NCM插件记录在第4节，YAML配置层及读写服务的当前结果单列于[配置验收](configuration.md)。旧测试数量、制品数量及依赖版本保留历史，不据早期限制否定后续已实现项。

启动步骤见[运行说明](../running.md)，依赖见[版本记录](../dependencies.md)，插件实测见[插件验证](plugins.md)，真实浏览器证据见 [Web 兼容记录](../../../web/docs/compatibility.md)。各仓库独立维护 Git 历史；提交签名与远端同步状态以当前仓库记录为准。

## 1. 逐步实现与提交

提交标题保留任务编号；`git log --oneline --all --grep=<编号>` 可查首次实现与后续修复。以下使用稳定阶段号索引，避免补签导致提交摘要变化后引用失效；插件的具体提交标题见[插件验证](plugins.md)，Web 步骤在其独立仓库记录。

| 步骤 | 已实现及验证范围 |
| --- | --- |
| S-P0-01 | Go 1.26.9、单 module、依赖固定；Windows/Linux 构建 |
| S-P0-02 | src/app 薄入口与按职责分模块 |
| S-P0-03 | 根 tests 布局、通过公开包接口测试 |
| S-P0-04 | 构建、格式、静态检查、覆盖率与竞争检查入口 |
| S-P0-05 | OpenAPI、JSON Schema、插件模型及成功/失败契约测试 |
| S-P0-06 | 可再生成的合成音频夹具与来源说明 |
| S-P1-01 | 历史基线为严格JSON配置、相对路径、目录分离与受控监听；后续已改用[YAML配置](../configuration.md)，当前不兼容旧格式 |
| S-P1-02 | 单数据目录锁、启动就绪、信号取消与有界退出 |
| S-P1-03 | HTTP 路由、请求限制、统一错误与控制面期限 |
| S-P1-04 | 管理凭据交换 Cookie、CSRF、Origin、限流、持久会话和撤销 |
| S-P1-05 | SQLite WAL、事务迁移、升级前备份、新版 schema 拒绝 |
| S-P1-06 | 持久任务、去重/幂等、单次领取、重启中断处理与安全错误 |
| S-P2-01 | 真 Local Files 子进程、握手、取消、退出和故障路径 |
| S-P2-02 | os.Root、限定资源引用、文件身份及版本校验 |
| S-P2-03 | 稳定来源 ID、完整扫描暂存后发布；离线/替换根不清库 |
| S-P2-04 | ffprobe 受限输入元数据、未知值保留 null、实际工具能力 |
| S-P2-05 | 来源/曲目查询、固定修订游标与管理扫描入口 |
| S-P3-01 | 分离 session/attempt/representation，生成私有固定输入副本 |
| S-P3-02 | 有限会话、空闲/总期限、撤销、停机与重启清理测试 |
| S-P3-03 | 精确登录会话授权、独立偏移、流取消与写超时 |
| S-P3-04 | 原流 HEAD/Range/ETag/If-Range、400/206/416 与真实 HTTP 验证 |
| S-P3-05 | attempt 并发代次、持久幂等、有限 seek/recover、停止 |
| S-P3-06 | 服务组合、同源静态 Web 与全新目录端到端启动 |
| S-P4-01 | 实际工具/profile 探测；限定 demuxer 防外部播放列表引用 |
| S-P4-02 | 任务限定二进制输入/输出通道、独立音频 Worker |
| S-P4-03 / S-P4-04 | 有界 PCM 数据契约、可替换 Go gain 节点与解码/编码管线 |
| S-P4-05 | timeRestart 新尝试、新 URL、串行定位与撤销期间拒绝创建 |
| S-P4-06 | 真实增益、位置标记、首块时序；另有 Edge 联合验收 |
| S-P4-07 | 处理并发、背压、任务期限及 Windows 进程树回收 |
| S-P4-08 | 输入固定副本、表示版本及权限隔离；处理输出不持久缓存 |
| PL-P0 / PL-P1 / PL-P2 | 版本化帧、能力、受控资源、正式 Worker、可选管理员固定 SHA-256 |
| S-P8 本次子集 | 可重复检查、三程序双平台构建、运行说明、真实浏览器矩阵与限制 |

## 2. 实际执行结果

| 环境/检查 | 结果与边界 |
| --- | --- |
| Windows amd64，Go 1.26.9 | gofmt、go vet、完整 Server 单元/契约/集成/端到端测试通过；设置真实 FFmpeg/ffprobe 路径 |
| Windows 并发竞争检查 | MSYS2 UCRT64 GCC 配合 CGO，`go test -race ./tests/...` 通过 |
| Windows 全新服务流程 | 实际扫描脚本、登录、列表、Range、跨会话拒绝、撤销、重启与退出通过 |
| 构建产物 | Windows amd64、Linux amd64 各生成 musicfs、musicfs-plugin-local、musicfs-plugin-audio-ffmpeg；输出在忽略的 build 目录 |
| Linux amd64 原生测试 | 基线当时在Arch WSL2、Go 1.26.9运行`go test ./...`与插件竞争检查通过；因缺FFmpeg跳过四项真实音频用例。后续补验见[Linux音频记录](configuration-linux-audio.md) |
| Linux 文件系统 | 测试使用 /mnt/d 的 Windows 磁盘挂载，不能推定 ext4 或 NAS 已验证 |
| 真实 Windows 音频 | Go gain -6.0206 dB 后 RMS 比例 0.5；定位误差约 23 ms；30 秒输入首块约 116 ms 且尚未处理完成；断连约 152 ms 回收 |
| Web 自动检查 | 23 项单元测试、6 项完整 Edge E2E 及定向键盘/滚动验证通过；生产构建通过 |
| Edge 154.0.4258.62 | 真实 WAV 解码非零 PCM、暂停/继续、原流定位、快速切歌/停止、鉴权恢复、撤销和分页通过 |
| Edge 处理播放 | AIFF 直放失败后，服务端解码→Go DSP→MP3 可播放；连续 2 秒→4 秒定位，FFT 标记符合目标音段 |
| Edge 600 秒 AIFF | 处理流响应约 468 ms，非零解码 PCM 约 1374 ms，均发生在输出流完成前；这是本机单轮观察值 |

最终覆盖率报告中 src 总体语句覆盖为 70.2%；覆盖率不代替真实浏览器/音频证据。检查脚本与构建使用 `-mod=readonly`，防止验证时静默改变依赖。真实工具测试未配置工具路径时会跳过，不能用跳过结果声称处理链通过。Windows 受限执行环境阻止本机音频通道时，真实集成验证在允许本机网络的执行环境完成。

浏览器截图与结构化观察值位于 Web 的 `docs/validation`；浏览器实际解码非零 PCM 已验证，但未进行人工扬声器听音。

## 3. 明确保留的边界

- 本基线当时使用JSON；后续[YAML配置层与读写服务](configuration.md)已完成，启动使用插件/实例显式开关；运行时动态管理及平滑停用租约仍未实现。官方Worker受管理员信任，不等于操作系统沙箱。
- 扫描增量请求目前执行受控完整复核；没有文件监视、自动重试调度、全面排除规则或自动熔断恢复。扫描异常不发布成功空库。
- “可用”只代表当前来源可读取，不能保证任意文件可解码。仅声明实测 WAV/MP3 原流与 MP3 处理 profile；其他浏览器、格式、ARM 和 macOS 未验收。
- 输入在创建会话时复制到有配额的私有磁盘，保持不可变字节；不把整曲装入内存。当前每次处理均流式生成，不缓存处理输出，因此没有完成持久转码缓存、跨会话复用和缓存发布系统。
- timeRestart 采用 100 ms 验收容忍范围；未承诺采样级定位或所有格式等同精度。处理流没有伪造 Content-Length、ETag 或原文件字节 Range。
- 基线当时未验证Linux真实FFmpeg及音频回收；2026-10-10的处理、定位及断连回收已补验，见[Linux音频记录](configuration-linux-audio.md)。物理NAS失挂/重挂、junction竞争、完整文件系统矩阵、Linux浏览器及人工听音仍不能由这些结果推定通过。
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

## 5. 后续授权增量：YAML配置与读写服务

本轮只实现配置迁移与读写服务，动态插件启停另行实施。旧JSON配置不保留兼容；旧迁移草案已由当前[配置说明](../configuration.md)替代，运行示例、启动器与文档同步使用YAML。详细证据见[配置验收](configuration.md)。

| 步骤 | 提交与完成范围 |
| --- | --- |
| S-P1-07 | Server `161cb1f`：严格YAML解析、编辑器Schema、显式插件/实例双层开关、多实例/多音乐根启动与扫描；正式示例改为YAML并验证 |
| S-P1-08 | Server `9aed3ef`：带修订、幂等和脱敏快照的配置读写服务，SQLite操作日志、平台原子替换及替换前后崩溃恢复 |
| 实例隔离复核 | Server `34d32a8`：配置加载阶段拒绝忽略大小写后重复的实例ID，防止Windows状态目录重叠；定向竞争检查与双平台重建通过 |
| S-P1-09 | Web `ff7d653`：E2E启动器实际生成YAML，三项真实Edge NCM回归通过；Server运行说明与过时配置文档同时清理 |

Windows全量测试及竞争检查、四程序Windows/Linux构建、Linux原生配置写入与YAML启动扫描/原流回归均已记录通过。2026-10-10补齐Linux FFmpeg/ffprobe后，YAML实际处理、PCM位置标记、启用边界及同YAML重启回归通过：两项E2E共41.185秒，真实音频及伪装播放列表隔离包44.075秒。最终Linux五个相关包完整回归、E2E/配置读写race，以及Windows新增YAML race和两平台NCM回归均通过，详见[Linux音频记录](configuration-linux-audio.md)。读写服务仅保存期望配置并报告待应用，不包含管理HTTP/CLI、文件自动监视或动态生命周期。

## 6. 后续授权增量：继续 Server 剩余阶段

2026-10-10，用户要求按步骤继续 Server 剩余部分并逐步提交，插件动态启停后置。下面只记录已完成步骤，P5—P8 的其他门槛仍以各章计划为准；没有扩展桌面 Client 或最简 Web 界面。

| 步骤 | 完成内容与证据 |
| --- | --- |
| S-P5-01 | 资产证据字段、申报/探测/估算/人工来源、持久代次、版本失效、历史审计与固定分页；HTTP/插件契约同步。Windows 完整检查及 race、Linux 全量测试及关键包 race 通过，含真实 VBR 扫描和原流播放，详见[P5验收](identification.md)。提交标题为 `feat(S-P5-01): 建立资产证据与版本失效模型` |
| S-P2-03 修复 | `76818aa`：真实大库发现的分页快照固定过期问题，改为空闲续期与一小时总寿命；双平台真实 Worker 验证通过，详见[扫描期限](scan-snapshot-lifetime.md) |
| S-P5-02 | 完整原始文件 SHA-512、系统稳定读取保护、持久队列/限速/取消/恢复、申报冲突与 HTTP 管理；Windows 全量/race、Linux 全量/核心 race 通过。按用户要求同次提交 import 顺序调整及依赖升级，最新依赖单独再验收，详见[P5验收](identification.md)。提交标题为 `feat(S-P5-02): 实现后台完整文件摘要并更新依赖` |
| S-P5-03 | 固定 Chromaprint 1.6.1 工具、独立后台规范化、版本保护、持久队列、指纹明细文件与可恢复引用、五个管理接口及启用降级；双平台真实音频及检查见[指纹验收](fingerprinting.md) |
| S-P5-04 | 有界候选比较、已验证摘要/兼容指纹/标签分层依据、人工关联/解除/拆分/排除、修订/幂等与原始目标审计；Windows/Linux 五个关键包竞争检查、真实服务重启与播放回归通过，见[人工关联验收](matching.md) |
| S-P6-01 | 歌词/封面实际内容校验、独立不可变文件、授权预览、按来源资产选择、人工锁/解锁与失效修订；Windows全量及race、Linux全量及关键包race均通过，见[媒体验收](media.md)。专辑/派生文件后续补齐见下行 |
| S-P6-02 | 八个发布/损坏边界重启、持久回收/二次引用检查、同快照备份引用、媒体复制和缺失清单；Windows/Linux vet 与六包 race 通过，见[存储验收](media-storage.md)。配置和部署管理入口属于 P8 |
| S-P6-03 | 独立本地 Scraper、受控内嵌/旁置读取、持久任务与预览、逐字段人工选用、NCM 输入及禁用后历史可读；Windows 全量/race、Linux 全量回归及九包 race 通过，见[Scraper验收](metadata.md) |
| S-P6-04 | 持久扫描触发、显式歌词导入与默认关闭的自动补缺、实例顺序/语言策略、保守候选依据、人工锁与崩溃幂等重放；Windows全量/race、Linux十包race及真实服务重启流程通过，见[自动补全验收](metadata-automatic.md) |
| S-P6-05 | 默认关闭的根/字段授权、Windows 本地 NTFS FLAC 可恢复写回、读写协调、稳定来源映射、新资产/摘要及人工绑定迁移；Windows 全量/race、Linux 关键 race、真实服务及恢复故障注入通过，见[写回验收](writeback.md)。Linux/NAS 不支持原件写回 |
| S-P6-01/S-P6-03 补齐 | 有序来源专辑/发行、独立封面与锁、专辑任务事务快照、受控缩略图及完整刮削文件；原始曲序、引用授权、重启历史读取、恢复/备份及原件不变通过。Windows完整检查和全量race、Linux vet及15包race通过；同时修复写回收尾误报恢复状态，见[专辑与派生文件验收](albums-media.md) |
| S-P2 导入选项增量 | 插件声明导入配置，实例默认值与单次覆盖，授权根选择、递归与深度、完整计划持久化、范围内缺失对账、串行代次及硬链接稳定身份；见[导入选项验收](plugin-import-options.md)。P6-04已实际接入歌词同步，当前声明为local-import-v2 |
| 真实音乐库验收 | 冻结快照双根完整扫描 1,943 条、重启标识稳定、6 个真实文件含 4 个 NCM 的 HTTP 播放/定位，以及后续正式 SHA-512 任务均通过；所有抽样原件前后不变，见[真实库记录](real-local-library.md) |
| S-P7-01 | 三平台官方文档、固定参考提交/许可、匿名探测和能力边界；已核对网易/QQ公开搜索及Apple无令牌拒绝，不将其视为账号或音频可用证明，见[平台调查](platform-research.md) |
| S-P7-02 | 三个独立正式在线Worker、作用域校验、受控网络、能力声明及YAML/HTTP接入；网易四项公开资料实际可用，QQ近期失败、Apple缺授权与三家音频不支持均明确返回。Windows与Linux定向race、正式HTTP公网只读实测和禁用重启通过，见[在线宿主验收](online-http.md)。未实现账号登录或在线音频，不伪造资产 |

| S-P7-03 | 有界匿名推荐聚合、账号/实例/配置隔离缓存、部分失败及旧结果、冷却与取消，HTTP和实际Worker/Server双平台race通过；修正UTC输出及不可靠短计时夹具，见[推荐验收](recommendations-http.md) |

P6本期门槛已通过；在线平台目前仅宣告已验收的公开资料能力，账号、完整音频和其他未列明能力不随此交付。歌单和完整部署继续分步推进。新增插件导入选项（歌词、递归与深度）均已接入实际扫描及补全流程。
