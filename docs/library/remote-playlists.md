# 显式导入与保守同步远程歌单

远程歌单引用、同步任务和本地修订持久保存。显式导入或建立关联会排队读取曲目并登记曲库来源。单独浏览推荐和歌单只读取资料。网易云使用已验证的平台账号，Bilibili 支持匿名公开歌单。

## 导入与同步

以下路径均以 `/api/v1` 开头，需要MusicFS登录。导入与同步需允许Origin、CSRF和8—128字符 `Idempotency-Key`；幂等窗口24小时。同键不同有效参数返回409，过期返回410，不生成另一份导入或任务。

1. `POST /remote-playlists` 提交 `instanceId`、`accountId`、`externalId`，返回 201 和 `{playlist,merged,jobId}`。需登录的平台使用实例当前验证的账号。
2. 登记歌单与安排同步任务在同一事务内完成。新导入在首次成功前显示待同步。通过 `jobId` 查询进度。重复导入同一歌单沿用原歌单编号。
3. `POST /remote-playlists/{id}/sync-jobs {"expectedBindingRevision":1}` 返回202 `{jobId,merged}`。从GET得到当前绑定修订；同歌单和有效配置的在途同步可合并。同键重放保留原任务编号，配置或有效参数变化明确冲突。
4. `GET /remote-playlist-sync-jobs/{id}` 读取 `queued/running/succeeded/failed/cancelled`。成功的resultRevision指向新本地快照；失败error为安全错误与本次HTTP requestId，没有上游原始正文。generation从入队0开始，领取或恢复时更新，用于拒绝旧任务结果，不是远端版本。
5. `POST /remote-playlist-sync-jobs/{id}/cancel {}` 自然幂等，不需要幂等键，仍校验Origin和CSRF。queued/running转cancelled并取消活动请求；已终态返回原结果。取消不删除上次成功快照。

宿主使用后台工作者分批获取曲目元数据，单个任务期限为 30 分钟。发布事务重新核对任务代次、用户、实例和绑定，并一起提交曲库曲目、来源、快照及关联目标的新增条目。平台已下架的成员保留编号及已知资料。失败的整次观测保留上次成功结果。升级后的首次启动会为已有导入及关联安排补齐任务。

遇到 `RATE_LIMITED` 时，运行中的任务先按提示等待并重试当前读取，保留本次已经取得的批次。重试仍受限时，服务端保存失败状态和可用的冷却秒数。队列空闲时会检查已过冷却期的任务，至少等待一分钟，再为原歌单安排同步。此前版本留下的限流失败也会恢复。歌单已关联「我的收藏」时，完整同步成功后曲目和来源一起入库。

## 阅读当前与历史

`GET /remote-playlists/{id}` 返回 `bindingRevision`、最新成功本地 `revision`、标题、stale、最后成功时间和错误。`GET /remote-playlists?after=…&limit=50` 列出本人显式导入的记录；limit为1—100，nextAfter为空表示本页末尾。列表是稳定编号顺序，不能保证并发变更时跨页固定快照。

`GET /remote-playlists/{id}/snapshots/{revision}?offset=0&limit=50` 固定一个本地修订读取条目，limit为1—100；nextOffset为空表示该修订结束。旧修订不可变，停用实例和服务重启后仍可读取本人历史；不存在与其他用户记录统一404。未知/重复参数、负offset、非规范整数或不存在的修订分别明确拒绝，不默认切换到最新快照。

历史快照条目保留出现顺序及稳定的 `remoteSourceId`。同一首歌在远程列表多次出现时分别记录位置，曲库则复用同一实例的来源记录。播放使用已登记的 `SourceTrack` 和当前账号的授权。

## 无固定远端版本时如何处理缺失

当前 `consistency=unversioned,remoteVersion=null`。complete只说明本次返回数量与申报一致，并非远端稳定版本；元数据摘要也不是音频版本。每次观测最多10000个条目，不完整、超时、限流、权限或网络失败均保留上次成功快照并标stale，不转换成空歌单。

一次成功完整观测中，出现的条目按远端顺序排列；首次缺失的旧出现保留在末尾，标记 `suspectedRemoved` 和首次缺失观测编号。后续时间更晚、完整且同绑定范围的观测仍缺失，才确认移除该出现；若又出现则恢复present。绑定配置修订变化时重新开始缺失确认，不能把旧配置下的缺失作为新配置的删除依据。包含待复核项的本地快照最多20000条，超额拒绝发布。

因此本地revision只表示MusicFS已原子发布的观测结果，不能声称同时固定了远端所有分页。旧快照仍可审计原出现，数据库保存确认移除所依据的观测编号。失败、取消和晚到的旧代次均不会改写成功快照。

## 边界与配置

复用[在线 Provider 配置](../providers/online.md#配置和实例状态)，停用保存后重启 Server 生效。本人已导入历史不要求来源在线；新导入与同步必须重新检查实例、账号和能力，不回退匿名或借用其他账号。导入与同步负责读取快照。将新增曲目追加到本人网易云歌单需要显式建立[账号歌单关联](source-playlists.md)。真实授权范围见[账号登录](../providers/platform-login.md)。[个人歌单聚合与隐藏](playlists.md)使用独立规则。

公开模型见 [OpenAPI](../contracts/openapi.yaml)和 [HTTP Schema](../contracts/http.schema.json)。
