# 来源专辑、发行与独立封面

来源专辑是同一所有者、同一 Provider 实例下的显式曲目集合；发行是本人来源专辑的显式组合。两者有稳定编号，不按名称、艺人或 Recording 自动合并。

## 创建和读取

登录后调用以下接口；所有修改都需要允许的 Origin、CSRF 和8—128字符 `Idempotency-Key`。

```http
POST /api/v1/source-albums
Content-Type: application/json

{"providerInstanceId":"b1548e96-3081-5c9c-8376-bf290e369d51","name":"我的来源专辑","sourceTrackIds":["来源曲目编号"]}
```

实例必须是当前用户已配置的 Provider，包括空专辑；创建不要求插件在线。成员最多1000条，必须属于同一用户和实例，不允许重复。成员顺序按请求保留。响应提供 `sourceAlbumId` 和从1开始的 `revision`。

```http
POST /api/v1/releases
Content-Type: application/json

{"name":"我的发行版本","sourceAlbumIds":["来源专辑编号"]}
```

发行最多包含256个本人来源专辑，保留提交顺序。同名的不同专辑、不同发行保持独立。`GET /source-albums/{id}`、`GET /releases/{id}` 读取；列表为 `GET /source-albums`、`GET /releases`，支持 `after`、`limit`，默认50、最多100。来源专辑列表可按 `providerInstanceId` 过滤。这是编号分页，不承诺跨请求固定快照。

`PUT /source-albums/{id}` 提交完整 `name/sourceTrackIds/expectedRevision`；`PUT /releases/{id}` 提交 `name/sourceAlbumIds/expectedRevision`。成员增删或重排推进修订；来源专辑成员变化同时推进引用它的发行修订，避免旧发行请求应用到变化后的来源集合。旧修订返回409。当前不提供删除操作。

## 选用独立封面

先通过[媒体接口](../metadata/media.md)上传成员当前资产的图片，或获取刮削候选的 `previewObjectId`。然后读取 `GET /source-albums/{id}/cover-selection`，初始 `revision=0,binding=null,manualLocked=false`。

```http
PUT /api/v1/source-albums/{id}/cover-selection
Content-Type: application/json

{"objectId":"图片对象编号","expectedRevision":0,"expectedTargetRevision":1}
```

发行对应 `/releases/{id}/cover-selection`。`expectedRevision` 是封面选择修订，`expectedTargetRevision` 是专辑/发行成员修订，两者独立。选用前验证当前成员、当前资产、图片文件和所有者；人工选择加锁。来源资产、成员或 Recording 后续变化不会清空已选封面，它仍保留原来源/资产依据。

`POST /{source-albums|releases}/{id}/cover-selection/unlock` 的正文是 `{"expectedRevision":1}`，只解除锁，不清空图片。历史和人工意图通过 `GET /{source-albums|releases}/{id}/changes` 读取，支持 `after` 序号及 `limit`。对象缺失或损坏通过原媒体读取链标记不可用；锁和历史仍保留。

## 按专辑刮削

```http
POST /api/v1/metadata-jobs
Content-Type: application/json

{"sourceAlbum":{"sourceAlbumId":"来源专辑编号","expectedRevision":1},"instanceIds":["d248bf34-1780-59d3-a557-9cf5d889e977"],"fields":["lyrics","artwork"],"languages":[]}
```

`sourceAlbum` 与非空 `sourceTrackIds` 二选一。入队事务固化原专辑修订、成员及每条当前资产/选择修订；空专辑或超过64成员明确拒绝，不截断。任务只处理入队快照中的目标；后续成员调整不扩展任务。同键重放原任务，新请求使用旧专辑修订返回冲突。逐目标候选仍按来源曲目选用，来源专辑封面需要另行明确选用。

完整执行记录和封面缩略图见[派生媒体](../metadata/media.md#派生内容)，公共字段以 [OpenAPI](../contracts/openapi.yaml) 为准。
