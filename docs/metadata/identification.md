# 资产识别与证据

来源曲目不需要先匹配 Recording 或计算摘要即可播放。摘要、指纹与人工关联各自保存证据，不扩大来源权限，也不证明跨来源时间轴兼容。

## 资产与公开字段

`GET /api/v1/source-tracks` 的每个条目包含 `asset`，描述该来源当前字节版本的证据。`sourceTrackId` 保持稳定，`assetId` 在来源字节版本变化后更新；原资产与证据保留为审计历史。歌词、封面、指纹明细等文件内容不放入该对象。

| 字段 | 语义 |
| --- | --- |
| `version` / `current` | 来源观测版本及是否仍代表当前文件；观测版本本身不是内容摘要 |
| `fileSizeBytes` | 原始文件完整长度，包括容器、标签和内嵌封面；未知为 null |
| `bitrateBps` / `bitrateMode` / `bitrateOrigin` | bit/s、cbr/vbr/unknown、选用值的来源；未知码率为 null，不能用 0 或音质档位名称替代 |
| `providerSha512` | Provider 申报的完整文件 SHA-512，不代表 Server 已验证 |
| `fileSha512` / `hashStatus` / `hashComputedAt` | Server 验证的完整文件摘要、状态和时间；摘要为 128 个小写十六进制字符，未验证时为 null |
| `hashConflict` | 申报摘要与已验证摘要不一致的当前冲突；包含两端证据引用，解决后为 null，历史仍保留 |
| `evidenceVersion` / `evidence` | 证据模型版本及当前有效证据，包含来源、算法版本、提取配置、覆盖范围和任务代次 |

NCM 的上述大小、版本和完整文件摘要属于原始加密容器；解密的 MP3/FLAC 是播放表示。解密后探测到的音频码率在证据配置中标明输入为 NCM 载荷，不把载荷摘要冒充原始文件摘要。

## 来源与状态

码率证据的 `origin` 分为 `provider`、`probe`、`estimate`、`manual`。当前视图按人工、实际探测、估算、Provider 申报的优先级选值，原申报保留。不能从一个平均码率推断 CBR/VBR；现有 ffprobe 适配器没有获得可靠模式时保留 `unknown`。

`hashStatus=unknown` 表示尚无计算结果；实际建立任务后才是 `pending`。成功且满足完整稳定读取条件时才能发布 `verified`；无法获得所需读取能力为 `unavailable`，计算失败为 `failed`。被新资产替代的历史视图为 `invalidated`，其旧摘要不再投影为当前文件事实。取消或失效任务不能晚到覆盖新结果。

摘要、指纹、Recording 关联及时间轴映射各自独立。相同完整字节可成为精确匹配依据，但不删除重复路径或合并权限；摘要不同不能单独证明录音不同。

## 存储与发布边界

扫描事务保存资产历史和申报/探测证据。相同版本重复扫描保留已验证摘要；文件变化或完整扫描确认来源消失后，旧资产及其未完成证据任务失效。失败、不完整或取消扫描继续保留上次成功音乐库。

Provider 本轮撤销的申报会退出当前证据；探测成功但缺值或改用其他来源时，对应旧探测/估算证据也会失效。探测失败或未尝试时，保留同一资产最后成功的探测证据；这些规则不覆盖人工值或完整文件摘要。

内部 `BeginEvidence` 为当前资产创建持久代次，`PublishEvidence` 在同一事务中复核用户、曲目、资产版本、算法/配置/范围和代次，`CancelEvidence` 结束待完成任务。Provider 输入不能指定 `verified` 或冒充宿主探测来源。摘要在数据库中另以 64 字节值保存，公开协议再转为小写十六进制。

证据更新生成新的 `libraryRevision`，已经发出的分页快照保持原内容；新列表读取新证据。历史查询按用户隔离并限制每页大小，不向公开列表附加私有路径。程序接口与 HTTP 模型以 [OpenAPI](../contracts/openapi.yaml) 和 [Schema](../contracts/http.schema.json) 为准。

## 后台完整文件摘要

完整摘要使用独立持久队列，通过标准库 SHA-512 顺序读取原始文件。每次读取最多 64 KiB，检查连续偏移、预期长度和末尾 EOF，不对解密载荷或 PCM 计算 `fileSha512`。空文件也必须先取得稳定读取保护。

默认 `hashing.auto: false`，扫描不会自动提交完整文件 SHA-512 验证任务。显式触发使用 `POST /api/v1/source-tracks/{id}/hash-jobs`（需要 `Idempotency-Key`），返回任务及是否复用；随后通过 `GET /api/v1/hash-jobs/{jobId}` 读取状态与字节进度。`POST /api/v1/hash-jobs/{jobId}/cancel` 自然幂等，重复取消不会重启任务。写请求沿用会话、Origin 和 CSRF 校验。管理流程由 API 完成，最简 Web 不增加操作页面。

启用自动计算后，只为本地 Provider 的当前资产排队；相同资产的活动任务合并，已验证结果复用。自动任务不会反复重试失败资产，手动请求使用新幂等键可重试。再次设为 `auto: false` 只停止发现新任务，已授权且持久排队的任务仍会恢复执行，可逐任务取消。默认最多一个任务、所有任务合计 4 MiB/s，手动任务优先于尚未开始的自动任务；这些是保守资源限制，不构成所有磁盘或 NAS 的性能承诺。配置范围见[配置说明](../operations/configuration.md)。

任务和证据一起提交，取消、资产失效及重启恢复会改变有效状态或代次，旧执行不能发布。计算完成后仍持有读取保护，在短事务中再次检查资产与代次；保护释放后发生的外部修改由后续观测识别为新版本。Server 关闭会停止读取，重启从文件起点恢复未完成任务，不能把旧进度拼接成摘要。

### 稳定读取的系统边界

Windows 通过 [ReOpenFile](https://learn.microsoft.com/en-us/windows/win32/api/winbase/nf-winbase-reopenfile) 重新打开已经授权的文件对象，只允许共享读取。已有写句柄、可写映射或无法取得保护时拒绝计算；保护期间不允许新写入和替换。Linux 使用 [读租约](https://man7.org/linux/man-pages/man2/F_SETLEASE.2const.html)，写入意图打断租约后放弃结果并及时释放，避免让外部写入等待内核超时。发布期间继续保持保护；文件路径被替换也会在校验时拒绝。

Windows 刷新本地索引时读取完整文件，以内容的 SHA-256 作为版本，文件身份单独保存。同长度的内容改写会生成新版本，仅更改时间戳或属性时版本保持稳定。日常播放直接读取当前文件，曲目详情显示最近一次扫描的观测。执行写回或发布资产证据时仍核对目标版本。从使用 NTFS 时间戳的旧版本升级后，下一次刷新索引会更新曲库记录。Linux 和 macOS 按文件身份、长度、mtime 和 inode 状态变更时间（ctime）计算版本。

完整文件 SHA-512 验证需要取得系统保护并读完整个文件。Linux 租约要求文件归属、权限及文件系统支持；NAS、其他挂载或 macOS 无法满足时返回 `unavailable`。后台摘要不可获得不影响普通播放。

当前仅本地来源明确声明完整稳定读取能力。在线来源没有因此获得隐式下载授权；后续 Provider 必须单独验证授权、能力和快照条件。申报摘要不参与精确字节匹配，申报错误保留 `PROVIDER_SHA512_MISMATCH` 审计并退出可依赖证据；实际摘要不被错误申报覆盖。相同摘要的多个路径继续保留独立来源与权限。

摘要与指纹用于[候选与人工关联](matching.md)，不根据申报摘要、标签或相似指纹自动合并。

## 本地音频指纹

### 工具与配置

当前由内置音频分析插件 `musicfs.plugin.analysis.chromaprint` 提取指纹，实现位于 [`plugins/analysis/chromaprint`](../../plugins/analysis/chromaprint)。插件直接在 Server 主进程执行，通过本地工具 `fpcalc` 1.6.1 的 CLI 算法 2 完成计算；`fpcalc` 是外部工具进程，不是独立的插件服务或插件 Worker。当前没有新增 BPM、调性等分析能力，也不包含外部检索或上传路径；没有向 AcoustID 等服务自动上传指纹。准确版本、源码提交、CLI 编号及许可证见[算法来源](../../src/metadata/identification/fingerprint/REFERENCES.md)。`fpcalc`、FFmpeg/ffprobe 均是显式外部依赖；工具二进制不提交到仓库。

宿主在组装时注入提取器，保留 [`fingerprint`](../../src/metadata/identification/fingerprint) 的证据模型、版本与明细校验，以及 [`fingerprintworker`](../../src/metadata/identification/fingerprintworker) 的授权、稳定读取、任务调度、取消和发布事务。插件只接受已绑定资产的读取输入并返回指纹明细，不自行修改资产、证据索引或任务状态。公共身份使用 `analyzer` 和 `decoder` 记录工具版本及摘要，算法名称、版本和参数版本由提取器声明；Chromaprint 对 fpcalc 1.6.1 的限制位于其插件实现。宿主继续验证固定的 PCM/uint32 明细规格、质量门槛和完整身份摘要，不接受跨不同算法、工具或配置身份的指纹比较。新增算法可实现现有 `Extractor` 契约；这不代表任意指纹二进制格式已经受支持。

数据库迁移 034 将旧 `fpcalc` 身份的指纹任务及索引原文归档到 `retired_fingerprint_evidence`，撤销旧派生证据和未完成发布，不在业务代码兼容旧 JSON。旧指纹任务编号不再作为当前任务查询，旧请求幂等记录失效；新请求重新提取。原音乐、歌曲身份、录音关系和其他业务数据保持不变。过期指纹文件由媒体回收流程处理，仍受备份引用保护。

YAML 的 `fingerprinting` 默认禁用。启用时指定 `fpcalcPath` 与实际文件的 `fpcalcSha256`，同时启用音频插件及其实例。路径相对配置文件解析，工具 SHA-256 和版本在启动/执行时校验。完整字段与有效示例见[配置说明](../operations/configuration.md)及[示例配置](../../configs/server.example.yaml)。并发默认为 1、最多 2；超时默认 30 秒、配置最多 120 秒。音频规范化自身仍有 30 秒上限。

未配置、版本不符、摘要变化或缺少原始 PCM 输出组件时，指纹能力明确不可用，不将工具安装失败变成整个播放服务不可用。外部修改 YAML 需重启应用；插件管理支持动态启用，停用保存后重启 Server 生效，见[插件管理](../plugins/plugin-management.md)。

### 提取与覆盖范围

请求指定 `startMs` 和 `durationMs`：起点为 0–86400000 毫秒，片段为 10000–120000 毫秒。Worker 从用户增益、均衡器、变速等处理之前的音频提取，单独规范化成 `s16le`、单声道、11025 Hz。两端按绝对时间向下量化为采样点，真实文件尾不补零；明细记录实际 `startSample`、`sampleCount`、工具版本/摘要与规范化版本。

NCM 使用独立解密插件读取内部音频，再进入同一规范化链；证据仍绑定原始 NCM 的资产和版本。原件通过稳定读保护保持到发布引用事务结束。资产变化、取消、旧任务代次或提取配置改变均不能发布当前有效的新证据。

任务成功表示提取及保存过程完成，是否可用于比较由 `quality.status` 说明：实际不足 10 秒为 `short`；少于 8 种指纹值或单值占比至少 90% 为 `lowInformation`；其余为 `available`。这些是保守的资源/信息门槛，不是识别准确率。即便两个不同纯音得到相同指纹，也不会因相同数组自动合并录音。

每份明细最多 64 KiB、原始指纹最多 1000 个 uint32。后台音频规范化有独立的固定 1 个执行通道，不消耗前台播放准入配额；等待受任务期限约束。这不代表操作系统 CPU、磁盘带宽或 NAS 吞吐的硬隔离。

### 文件与恢复

完整指纹明细放在 `mediaRoot`，数据库只保存对象摘要、索引、覆盖范围和任务状态。对象编号和相对位置由服务生成，公开响应不包含媒体目录、工具路径或音乐路径。

发布顺序为持久 `prepared` 记录、暂存写入与校验、`written`、不可变文件发布、`published`、事务切换索引及任务结果、`referenced`、`finished`。文件系统与数据库不是同一个原子事务。重启后未引用的指纹发布不会沿用已经丢失的原件读保护：该发布终止，任务重新取得版本保护并计算；已引用内容经验证后完成收尾。缺失或损坏明细明确失效，保留审计并允许重算，不阻塞普通播放。回收与引用保护见[媒体存储](media.md#发布回收与备份)。

### 管理接口

沿用会话 Cookie、Origin 和 CSRF 校验。创建任务还要求 `Idempotency-Key`；同一键不能改参数。同一用户/资产/工具身份/范围的活动任务合并，已有有效结果可复用。所有读取按用户归属检查，不能查询其他用户的私人来源。

| 接口 | 用途 |
| --- | --- |
| `POST /api/v1/source-tracks/{id}/fingerprint-jobs` | 正文如 `{"startMs":0,"durationMs":30000}`，返回 202 与 `jobId` |
| `GET /api/v1/fingerprint-jobs/{id}` | 状态、失败原因、工具身份和可空结果索引 |
| `POST /api/v1/fingerprint-jobs/{id}/cancel` | 正文 `{}`，自然幂等，已完成结果不会被取消改写 |
| `GET /api/v1/source-tracks/{id}/fingerprints` | 当前资产有效索引，`limit` 为 1–100，按 `nextAfter` 继续读取 |
| `GET /api/v1/fingerprints/{id}` | 显式读取有权访问的明细；历史索引不能当作当前资产证据 |

列表采用有界键集分页；它不是库列表的固定快照，读取期间新发布的指纹可能在下一轮刷新出现。任务/明细由 API 和测试客户端管理，最小 Web 没有增加指纹管理页面。[候选与人工关联](matching.md)中，只有明确的人工操作才改变录音关联，提取本身不合并曲目。
