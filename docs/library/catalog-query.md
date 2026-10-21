# 本地曲库查询与固定结果

本页描述已实现的 Server 接口；权威字段和约束见 `contracts/openapi.yaml` 与 `contracts/http.schema.json`。本地查询只覆盖当前用户拥有的已扫描来源，不混入在线资料或伪造播放权限。

## 1. 查询入口

- `GET /api/v1/library/search`：按 `type=tracks|albums|artists` 独立查询。首请求建立完整不可变结果；后续传相同规范化条件与 `cursor`。
- `POST /api/v1/library/queries`：正文 `{ "query": { "type": "tracks" }, "limit": 50 }`，要求会话、CSRF 与 `Idempotency-Key`；固定全部匹配结果供播放全部／追加队列逐页读取。
- `GET /api/v1/library/queries/{id}`：用查询 ID 恢复同一结果；不传游标从第一页恢复，传 `cursor` 继续，`limit` 为 1–200。
- `DELETE /api/v1/library/queries/{id}`：取消并释放结果；重复取消成功，其他用户不可读或取消。
- `GET /api/v1/library/facets?providerInstanceId=...`：读取完整库的目录、格式、流派、年份、采样率及各元数据未知项数量。
- `GET /api/v1/source-artists/{id}`：本地来源歌手署名详情。相关专辑／曲目分别查询 `type=albums|tracks&artistId=...`。专辑曲目使用 `sourceAlbumId` 条件，专辑详情继续使用既有 `/source-albums/{id}`。

原 `/source-tracks` 保持基础来源浏览用途。新页面的组合过滤、分类和排序应统一走本页接口，不能先取一页再在浏览器中过滤。

## 2. 条件与顺序

条件之间取交集；多选条件内部取并集。`genres`、`formats`、`years`、`sampleRatesHz`、`availability` 和 `unknownFields` 在 JSON 中为数组，GET 查询参数使用重复同名键。

| 条件 | 语义 |
| --- | --- |
| `providerInstanceId` | 单一用户来源；省略查询用户全部本地来源 |
| `q` | 去除首尾空白、大小写不敏感、各空白分词均须命中；曲目匹配展示名／标题／歌手／真实专辑标签和显式来源专辑名，专辑匹配名称／歌手，歌手匹配署名 |
| `artistId`、`sourceAlbumId` | 稳定来源身份；可组合 |
| `rootId`、`directoryId` | 授权根／不透明目录身份；目录包含后代，不接受物理路径 |
| `format`、`formats` | 已观测容器格式；容器未知时使用 codec；单值与多值同时出现仍取交集 |
| `genres`、`years`、`sampleRatesHz` | 真实标签或探测数值；没有数据的项不匹配已知值 |
| `favoriteOnly` | 当前账号收藏集合；创建查询时固定 |
| `availableOnly`、`availability` | 扫描观测的来源可用状态；不是当前音频解码、实例在线或播放授权承诺 |
| `minBitrateBps`、`maxBitrateBps` | 含边界的码率范围；未知值不匹配 |
| `unknownFields` | `artist`、`album`、`genre`、`year`、`sampleRateHz`、`bitrate`、`format`；多个未知条件取交集 |

曲目 `sort` 支持 `name`、`title`、`artist`、`album`、`duration`、`bitrate`；专辑和歌手支持 `name`。`order` 为 `asc|desc`，默认 `name/asc`。字符串以大小写折叠后的码点顺序排序；同值使用稳定 ID 升序兜底。排序在完整结果上执行，页大小不改变集合或顺序。

扫描通过真实音频探测补采 `album`、`genre`、`year`、`sampleRateHz`；缺失或非法值为 null。NCM 专辑可来自解密后的元数据，采样率始终来自音频载荷实际探测。没有更新扫描的既有记录也按未知处理。

`playableOnly` 暂无可靠的统一静态判定，列在 `unsupportedFields` 中；请求该参数会明确拒绝。UI 可使用观测可用状态筛选，但不能把它命名为“仅可播放”。播放描述符仍负责即时授权、源版本和格式验证。

## 3. 身份、封面与目录隐私

曲目结果保留 `sourceTrackId` 与既有 `SourceRef`（本地 ID 有值、远程 ID 为 null），不跨来源同名合并。结果中的 `rootId`、`directoryId` 是授权命名空间身份，不包含物理路径。

歌手实体的 `identityKind=localCredit`：它表示“同一用户、同一 Provider 内的精确署名标签”，并非已验证的真实人物。两个 Provider 的相同署名拥有不同 ID；不会把它们与平台艺人实体合并。头像未知返回 null。

专辑搜索只返回已建立的显式 `SourceAlbum`，同名专辑仍保持独立身份。曲目标签的专辑名称可搜索，但不会仅凭标签同名自动合并成 `SourceAlbum`。专辑封面返回当前绑定的受鉴权媒体对象 ID；失效的目标版本或不可用对象不作为当前封面。

目录分类返回稳定哈希 ID、父 ID、根 ID、Provider ID 和末级目录名称；根显示为 `Root`，前端可用已授权 Provider 根名称替换。响应不返回绝对路径或相对路径串。

## 4. 固定集合、恢复和取消

每个页面带 `queryId`、规范化 `query`、`catalogRevision`、`expiresAt`、准确 `total`、`items` 与 `nextCursor`。快照同时固定曲库、专辑和收藏视图；创建后的扫描、元数据更新、收藏变更不会混入旧页。

快照有效期 10 分钟，读取不续期。游标绑定用户、查询 ID 和完整条件；不同用户、条件变更、跨查询游标、已取消或过期均不能继续原集合。游标失效返回 `CURSOR_EXPIRED`，前端须提示刷新；不能静默把新首批拼到旧结果。进程重启后仍可在剩余有效期内恢复。

“播放全部”应逐页收集固定结果中的稳定来源引用，直到 `nextCursor=null` 才算完整；缺失源仍在结果中保留其观测状态。实际开始播放时若源变更、插件下线或格式不支持，按播放接口失败跳过并汇总，不能把搜索命中视为已获播放权。用户取消时取消 HTTP 请求，并删除已创建查询释放服务端结果。

每用户最多 64 个活动查询，单次源记录上限 100,000，单快照最多 64 MiB，同用户保留结果最多 128 MiB；超出返回 `RATE_LIMITED`，不会截断后谎报完整。幂等键在快照过期后的 24 小时内保留，重复创建同条件返回原查询，修改条件返回 `CONFLICT`，过期键返回 `CURSOR_EXPIRED`。
