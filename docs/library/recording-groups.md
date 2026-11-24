# 按录音显示曲库

曲目列表按录音（Recording）归组，每首可以展开查看不同版本与来源。用户可以手动关联或解除来源。网易云新来源首次入库时也能自动归入已有本地录音，具体条件见[重复识别](duplicate-detection.md)。

## 查询

```http
GET /api/v1/source-tracks?groupBy=recording&limit=50
```

保留普通曲目页的 `items`、`nextCursor`、`libraryRevision`，另返回非负整数 `matchingRevision`。每项仍为可播放的代表来源曲目，增加：

- `recordingId` 是来源所属录音的 ID，尚未关联或已解除关联的来源为 `null`。
- `versions`：该录音全部本人来源曲目，包含代表来源。尚未关联的项目包含一个版本。成员不嵌套 `versions`。

代表来源优先使用用户为录音指定的版本。自动策略先比较可用性，再按已知码率、文件大小降序排列，最后以显示名称与来源 ID 定序。展开后可以明确播放其他版本。歌单条目通过可选的强制来源独立指定版本。[曲目资料、歌单与播放记录](music-catalog.md)

分组前读取本用户的全部来源，因此同一录音的成员即使原先分布在不同曲目页，也会完整出现在 `versions`。`providerInstanceId` 过滤选择包含该来源实例的组；组内仍返回所有本人来源，避免隐藏其他版本。未关联来源按各自的 `sourceTrackId` 独立显示。

## 分页与人工修订

首次查询在同一事务中固定曲库、人工关联修订和所有组成员。游标绑定用户、来源过滤和 `groupBy=recording` 模式，不能用于普通曲目页或其他用户。后续页面继续读取相同快照，保存十分钟；过期或绑定不符返回 `CURSOR_EXPIRED`，应从首屏重新读取。

人工合并使用既有 `POST /matching/links` 与 `expectedRevision`。客户端可采用聚合页的 `matchingRevision`；操作成功后重新加载聚合曲库。旧页仍保持其原修订，新决定不会悄悄混入旧游标；提交旧修订时返回 `CONFLICT`，应重新读取并由用户确认。解除和拆分也遵循同样规则。

每页最多 200 个组，每个 Recording 最多 256 个来源；快照最多 100,000 个来源、64 MiB。超限明确返回 `RATE_LIMITED`，不会静默截断成员。普通查询省略 `groupBy` 时继续逐来源分页，不改变既有客户端行为。

人工决定与权限规则详见[本地候选与人工录音关联](../metadata/matching.md)。
