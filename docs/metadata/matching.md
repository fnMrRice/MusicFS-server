# 本地候选与人工录音关联

候选辅助人工判断，确认后保存 Recording 与来源曲目的关联。未匹配来源继续可播放；人工操作不删除来源、改写原文件、合并权限或创建跨来源时间轴证据。当前通过管理 API 操作。

候选可以依据当前资产的已验证完整 SHA-512、本实例保存的兼容局部指纹，以及标题、歌手和时长。Provider 申报摘要不作为已验证摘要使用。候选的 `score` 是审核排序分数，指纹 BER 是比较指标，两者都不是“同一录音”的概率；所有候选均 `requiresConfirmation=true`。同名、现场、混音和相似纯音可能造成歧义，仍由用户确认。

## 内置评分插件与宿主边界

默认评分实现位于 [`plugins/matching/standard`](../../plugins/matching/standard)，插件标识为 `musicfs.plugin.matching.standard`。Server 在组装时通过 `candidates.Scorer` 接口注入，并在主进程直接调用；不启动评分插件子进程。本次拆分保留 `local-candidates-v1` 算法版本、既有评分公式、指纹比较预算及确定性排序。

宿主负责来源归属、快照、输入限额和人工排除；评分插件只收到过滤后的候选证据，返回分数和理由。返回值还须通过宿主的资产绑定、数量、算法版本和人工确认标记检查。保存事务再次复核当前证据及人工修订，哈希和标签候选通过宿主公共契约校验，数据库插件不依赖具体评分插件。

人工关联、拆分、解除、排除及审计始终属于宿主业务。评分插件不能自动确认或合并来源，也不能创建跨来源时间轴证据。

## 候选查询

路径均相对于 `/api/v1`。先通过正常会话登录；写请求携带允许的 `Origin`、Cookie 与 `X-CSRF-Token`，请求正文为 JSON。来源编号来自本人 `GET /source-tracks`，不能传路径、内部资产快照或自称已验证的证据。

对一个来源刷新候选：

```http
POST /source-tracks/{sourceTrackId}/match-candidates/refresh
Content-Type: application/json

{"sourceTrackIds":["another-source-id"]}
```

显式比较池最多256个不重复来源，不包含当前锚点；选择不属于本用户的来源返回 `NOT_FOUND`。省略 `sourceTrackIds` 或传空数组时，宿主选择最多256个本人来源，不表示完整搜索任意大小的曲库。该刷新操作自然幂等，不需要 `Idempotency-Key`：相同修订、资产/证据绑定和结果复用候选 ID 与创建时间，不推进人工关联修订。单实例最多一次比较、最长10秒；发布前再次校验快照，发生变化时返回冲突并重新刷新。

读取 `GET /source-tracks/{id}/match-candidates?limit=20&after=…`。默认20条，上限100条，按分数降序、ID升序；后续使用返回的 `nextAfter`，`null` 表示当前分页结束。普通列表只返回当前 `pending` 候选；排除、已关联和陈旧候选不再作为当前建议。游标不存在或不属于该用户/来源时返回 `CURSOR_EXPIRED`，应从首屏重建。分页不是固定快照，刷新或人工决定后应重新读取，不能把旧页与新结果当作同一快照。

未启用指纹提取仍可按可用标签或已验证摘要产生候选；没有足够证据时返回空列表，不能将“无候选”解释为已证明录音不同。本功能只在本实例处理已有证据，不触发外部上传或平台搜索。

## 人工确认与撤销

先读取 `GET /matching/revision` 的本人全局修订；初始为0。每个新的人工决定使用新的 `Idempotency-Key`，并把最新修订填入必需的非负整数 `expectedRevision`。并发操作使修订变化时返回 `CONFLICT`；重新读取当前状态后由用户判断，不自动覆盖另一项决定。以下四个接口均返回 `Mutation`：新修订、操作 ID、可空的录音 ID 和逐项审计。

| 操作 | POST 路径 | 请求字段 |
| --- | --- | --- |
| 关联 | `/matching/links` | `expectedRevision`、1—256个 `sourceTrackIds`；可选 `targetRecordingId`，省略/null表示创建录音 |
| 解除 | `/matching/unlinks` | `expectedRevision`、`sourceTrackId` |
| 拆分 | `/matching/splits` | `expectedRevision`、`recordingId`、该录音成员的非空真子集 `sourceTrackIds` |
| 排除/撤销排除 | `/matching/exclusions` | `expectedRevision`、`leftSourceTrackId`、`rightSourceTrackId`、必需布尔值 `excluded` |

例如，首次人工确认两项来源属于同一录音：

```json
{"expectedRevision":0,"sourceTrackIds":["source-one","source-two"],"targetRecordingId":null}
```

将来源加入既有录音时只移动明确选中的来源，不把它原先录音的其他成员隐式合并。每个录音最多256个成员。关联不能越过排除关系；需要先明确撤销排除。仍处于同一录音的两项来源不能直接设为排除，应先解除或拆分，接口不会暗中替用户修改既有关联。

解除后保留明确的 `unlinked` 人工决策，区别于从未决策的 `unmatched`。拆分创建新录音并保留每项来源原目标和新目标。相同幂等键及相同规范化请求在24小时窗口内重放原操作，不重复生成录音、推进修订或增加审计；同键改变请求返回冲突，过期返回 `IDEMPOTENCY_EXPIRED`。重放原操作返回的是当时结果，之后查询当前修订与关联以查看最新状态。

## 查询与审计

`GET /source-tracks/{id}/recording-link` 返回本人来源的录音 ID、`unmatched/linked/unlinked` 决策和当前全局修订。`GET /recordings/{id}` 返回本人录音当前成员、创建/更新时间和当前修订。其他用户与不存在的来源、录音或候选统一不可读取。

`GET /matching/changes?after=0&limit=50` 按本用户递增的 `sequence` 分页，默认50条、上限200条。每项包含操作 ID、类型、修订、来源及 `fromRecordingId/toRecordingId`，或排除关系两端与布尔值。返回 `nextAfter` 供继续读取；`null` 表示本次查询已读完，后续有新决定时可从最后已读序列查询。

审计保留原目标，供[歌单隐藏规则](../library/playlists.md#隐藏意图与确认)处理关联扩大和拆分。关联本身不生成 `TimelineEvidence`，也不承诺跨来源续播。精确字段以 [OpenAPI](../contracts/openapi.yaml) 与 [HTTP Schema](../contracts/http.schema.json) 为准。
