# 资产识别与证据

本说明跟随 [P5 实施步骤](implementation/05-identification.md)更新。来源曲目不需要先匹配 Recording 或计算摘要即可播放；识别结果不授予额外来源权限，也不证明跨来源时间轴兼容。

## 资产与公开字段

`GET /api/v1/source-tracks` 的每个条目包含 `asset`，描述该来源当前字节版本的证据。`sourceTrackId` 保持稳定，`assetId` 在来源字节版本变化后更新；原资产与证据保留为审计历史。歌词、封面、指纹明细等文件内容不放入该对象。

| 字段 | 语义 |
| --- | --- |
| `version` / `current` | 来源观测版本及是否仍代表当前文件；观测版本本身不是内容摘要 |
| `fileSizeBytes` | 原始文件完整长度，包括容器、标签和内嵌封面；未知为 null |
| `bitrateBps` / `bitrateMode` / `bitrateOrigin` | bit/s、cbr/vbr/unknown、选用值的来源；未知码率为 null，不能用 0 或音质档位名称替代 |
| `providerSha512` | Provider 申报的完整文件 SHA-512，不代表 Server 已验证 |
| `fileSha512` / `hashStatus` / `hashComputedAt` | Server 验证的完整文件摘要、状态和时间；摘要为 128 个小写十六进制字符，未验证时为 null |
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

证据更新生成新的 `libraryRevision`，已经发出的分页快照保持原内容；新列表读取新证据。历史查询按用户隔离并限制每页大小，不向公开列表附加私有路径。程序接口与 HTTP 模型以 [OpenAPI](contracts/openapi.yaml) 和 [Schema](contracts/http.schema.json) 为准。

## 分步实现范围

S-P5-01 建立上述证据模型、扫描持久化、失效规则与任务发布边界。后台完整 SHA-512、指纹工具、候选与人工关联分别按 S-P5-02—04 实施，不能仅因模型中有相应字段就视为对应计算能力已交付。
