# 显式导入与保守同步远程歌单

S-P7-04 将远程歌单引用、同步任务和本地修订持久保存。读取[推荐](recommendations.md)或[在线资料](online.md)不产生导入；必须明确提交导入请求。首版接入已验证的网易公开歌单观测，账号为空匿名；QQ/Apple尚无可用的歌单导入能力。远程曲目只有独立资料引用，不创建音频Asset，也不意味着可以播放。

## 导入与同步

以下路径均以 `/api/v1` 开头，需要MusicFS登录。导入与同步需允许Origin、CSRF和8—128字符 `Idempotency-Key`；幂等窗口24小时。同键不同有效参数返回409，过期返回410，不生成另一份导入或任务。

1. `POST /remote-playlists {"instanceId":"netease-public","accountId":"","externalId":"91"}` 返回201 `{playlist,merged}`。账号字段必须显式提供空字符串；歌单编号来自该来源资料，不接受URL、路径、配置修订或凭据。
2. 导入只记录意图并验证当前实例/能力，不访问远端。初始 `revision=0,title="",stale=true,lastSuccessAt=null,error=null`，尚无快照。已有本人相同实例/账号/歌单返回同一playlistId，`merged=true`；重放返回该身份当前的描述，不把旧描述冻结为永久状态。
3. `POST /remote-playlists/{id}/sync-jobs {"expectedBindingRevision":1}` 返回202 `{jobId,merged}`。从GET得到当前绑定修订；同歌单和有效配置的在途同步可合并。同键重放保留原任务编号，配置或有效参数变化明确冲突。
4. `GET /remote-playlist-sync-jobs/{id}` 读取 `queued/running/succeeded/failed/cancelled`。成功的resultRevision指向新本地快照；失败error为安全错误与本次HTTP requestId，没有上游原始正文。generation从入队0开始，领取或恢复时更新，用于拒绝旧任务结果，不是远端版本。
5. `POST /remote-playlist-sync-jobs/{id}/cancel {}` 自然幂等，不需要幂等键，仍校验Origin和CSRF。queued/running转cancelled并取消活动请求；已终态返回原结果。取消不删除上次成功快照。

宿主组合使用一个后台同步工作者，最多1024个queued/running任务。任务总期限30秒，在线调用另受宿主10秒期限、准入和Worker速率约束。重启恢复未完成任务时更换代次，执行前重新核对owner/实例/匿名账号/平台/实际程序配置修订，发布事务再次验证任务状态、代次和绑定。保存停用不改变当前任务租约；重启后已禁用或实际配置已变化时，旧任务不能凭旧绑定继续发布。

## 阅读当前与历史

`GET /remote-playlists/{id}` 返回 `bindingRevision`、最新成功本地 `revision`、标题、stale、最后成功时间和错误。`GET /remote-playlists?after=…&limit=50` 列出本人显式导入的记录；limit为1—100，nextAfter为空表示本页末尾。列表是稳定编号顺序，不能保证并发变更时跨页固定快照。

`GET /remote-playlists/{id}/snapshots/{revision}?offset=0&limit=50` 固定一个本地修订读取条目，limit为1—100；nextOffset为空表示该修订结束。旧修订不可变，停用实例和服务重启后仍可读取本人历史；不存在与其他用户记录统一404。未知/重复参数、负offset、非规范整数或不存在的修订分别明确拒绝，不默认切换到最新快照。

条目包含独立entryId、position、remoteSource引用、state、lastObservedPosition和可空firstMissingObservation。同一远程歌曲重复出现时保留不同entryId和位置，共用稳定remoteSourceId；上游没有稳定出现编号时，按歌曲编号与该歌曲出现次序保留身份，无法区分内容完全相同的两次出现。remoteSource固定 `playbackAvailability=unsupported`，没有assetId、codec、媒体URL或文件摘要。

## 无固定远端版本时如何处理缺失

当前 `consistency=unversioned,remoteVersion=null`。complete只说明本次返回数量与申报一致，并非远端稳定版本；元数据摘要也不是音频版本。每次观测最多10000个条目，不完整、超时、限流、权限或网络失败均保留上次成功快照并标stale，不转换成空歌单。

一次成功完整观测中，出现的条目按远端顺序排列；首次缺失的旧出现保留在末尾，标记 `suspectedRemoved` 和首次缺失观测编号。后续时间更晚、完整且同绑定范围的观测仍缺失，才确认移除该出现；若又出现则恢复present。绑定配置修订变化时重新开始缺失确认，不能把旧配置下的缺失作为新配置的删除依据。包含待复核项的本地快照最多20000条，超额拒绝发布。

因此本地revision只表示MusicFS已原子发布的观测结果，不能声称同时固定了远端所有分页。旧快照仍可审计原出现，数据库保存确认移除所依据的观测编号。失败、取消和晚到的旧代次均不会改写成功快照。

## 边界与配置

不增加新的YAML开关，复用现有已启用的[在线Provider配置](online.md#配置和实例状态)，停用保存后重启 Server 生效。读取历史不要求来源在线；新导入与同步必须重新授权可用实例，非空账号明确不支持，不换用其他来源。当前没有平台写回、账号登录、音频解析或Web歌单管理页；[个人歌单聚合与隐藏](playlists.md)使用独立规则，不由同步隐式处理。

公开模型见[OpenAPI](contracts/openapi.yaml)和[HTTP Schema](contracts/http.schema.json)。真实Server、持久化、并发和平台验证的不同范围见[远程歌单接口验收](validation/remote-playlists-http.md)。
