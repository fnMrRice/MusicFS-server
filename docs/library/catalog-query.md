# 本地曲库查询与固定结果

本页描述已实现的 Server 接口；权威字段和约束见 `contracts/openapi.yaml` 与 `contracts/http.schema.json`。本地查询只覆盖当前用户拥有的已扫描来源，不混入在线资料或伪造播放权限。

## 1. 查询入口

- `GET /api/v1/library/search`：按 `type=tracks|albums|artists` 独立查询。首请求建立完整不可变结果；后续传相同规范化条件与 `cursor`。
- `GET /api/v1/library/categories?categoryKind=artist|album|genre|folder`：曲目分类集合，使用同一固定查询分页；也可通过 `library/search?type=categories` 读取。
- `POST /api/v1/library/queries`：正文 `{ "query": { "type": "tracks" }, "limit": 50 }`，要求会话、CSRF 与 `Idempotency-Key`；固定全部匹配结果供播放全部／追加队列逐页读取。
- `GET /api/v1/library/queries/{id}`：用查询 ID 恢复同一结果；不传游标从第一页恢复，传 `cursor` 继续，`limit` 为 1–200。
- `DELETE /api/v1/library/queries/{id}`：取消并释放结果；重复取消成功，其他用户不可读或取消。
- `GET /api/v1/library/facets?providerInstanceId=...`：读取完整库的目录、格式、流派、年份、采样率及各元数据未知项数量。
- `GET /api/v1/source-artists/{id}`：本地来源歌手署名详情。相关专辑／曲目分别查询 `type=albums|tracks&artistId=...`。专辑曲目使用 `sourceAlbumId` 条件，专辑详情继续使用既有 `/source-albums/{id}`。

原 `/source-tracks` 保持基础来源浏览用途。新页面的组合过滤、分类和排序应统一走本页接口，不能先取一页再在浏览器中过滤。

### 普通音乐范围

曲目浏览统一排除当前用户已确认归属 ASMR 作品（`contentType=asmr`）或 CD 抓轨副本（`kind=cd_dump`）的来源音频。普通音乐的 `directory` 副本仍可显示，不根据目录名、标签、CUE 或编码格式推测归属。规则在分页、Recording 聚合、分类和筛选项统计之前应用；隐藏成员不会出现在聚合版本或“播放全部”的新查询结果中。

归属以已发布副本的音频节点为准，匹配来源曲目编号，或同用户、来源、根和精确文件相对位置。先扫描再导入的同一音频也会排除，其他用户／来源／根下的同路径文件不受影响；未收录进副本的文件不因目录前缀相同而被隐藏。

专辑、歌手、流派和文件夹只统计可见音乐；仅包含被排除音轨的分类不显示，共用分类及祖先目录保留普通音乐成员。作品详情仍使用原来源身份读取和播放完整音轨，附件、收藏及已保存歌单不因浏览范围调整而删除。预览不改变范围，确认导入或重新标记副本后，新建查询采用当前归属；已有固定查询仍遵循下文的快照规则。

`GET /api/v1/source-tracks/{id}` 按明确的来源编号读取当前用户音轨，供歌单等显式引用解析，包含普通曲库不展示的 ASMR／CD 抓轨音频。返回既有 `SourceTrack` 模型；不存在或属于其他用户返回 `NOT_FOUND`，不公开物理路径，也不代替播放时的授权与版本检查。客户端不得用普通曲库列表是否含有该编号来判断歌单条目是否存在。

### 曲目分类浏览

分类条目包含 `categoryId`、`kind`、`name`、`unknown`、`providerInstanceId`、`rootId`、`parentId`、`trackCount` 和 `breadcrumbs`。`categoryId` 是用户范围内的稳定不透明编号；分类集合支持来源、关键词和既有筛选条件，排序后分页。数量统计满足筛选的来源曲目数，同一曲目的重复署名不重复计数。

- 歌手按同一来源内的精确署名分类，一首曲目可属于多个歌手。
- 专辑优先采用曲目标签，标签缺失时采用显式来源专辑的名称；同一来源内同名标签形成浏览分类，不据此创建或合并 `SourceAlbum`。
- 流派忽略首尾空白及大小写，可跨来源汇总；缺失、空白歌手／专辑／流派有独立的未知分类，不与字面名称为“未知”的标签混合。
- 文件夹不传 `parentId` 时返回音乐根，指定后只返回直接子目录；仅从已扫描记录中的有效根及安全相对路径构造，不接受磁盘路径。`trackCount` 包含后代；`breadcrumbs` 从根到自身，只含编号和名称，根名称 `Root` 可用来源根的显示名替换。

用 `GET /library/categories?categoryKind=...&categoryId=...` 恢复单个分类及面包屑；与 `parentId` 互斥。不存在或超出当前用户、来源范围时返回 `NOT_FOUND`。进入分类后使用 `GET /library/search?type=tracks&categoryKind=...&categoryId=...`，目录成员包含子树。先验证分类身份，再叠加筛选；合法分类的筛选无结果返回空页。分类查询返回来源曲目，不进行 Recording 聚合；“全部曲目”的既有 Recording 聚合接口保持原语义。

## 2. 条件与顺序

条件之间取交集；多选条件内部取并集。`genres`、`formats`、`years`、`sampleRatesHz`、`availability` 和 `unknownFields` 在 JSON 中为数组，GET 查询参数使用重复同名键。

| 条件 | 语义 |
| --- | --- |
| `providerInstanceId` | 单一用户来源；省略查询用户全部本地来源 |
| `q` | 去除首尾空白、大小写不敏感、各空白分词均须命中；曲目匹配展示名／标题／歌手／真实专辑标签和显式来源专辑名，专辑匹配名称／歌手，歌手匹配署名 |
| `artistId`、`sourceAlbumId` | 稳定来源身份；可组合 |
| `categoryKind`、`categoryId` | 分类内曲目查询须成对提供；不能将分类编号作为来源实体编号 |
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

曲目列表、Recording 聚合成员及分类曲目均返回可空的 `coverObjectId`。未合并曲目使用当前资产版本已选用且可用的封面；已合并曲目的顶层封面优先采用 Recording 人工偏好，否则使用当前成员中像素面积较大、短边较大的图片，同尺寸以对象编号稳定排序。展开的 `versions` 保留每个来源自己的封面。客户端通过 `GET /api/v1/media/{objectId}` 读取受鉴权图片，不需要逐首请求选用详情。封面选择在返回每页时批量校验，固定快照的曲目成员与顺序仍保持，分组快照不会取用当时尚未加入的成员图片。图片缺失或读取失败显示占位图。扫描提取的封面保存在部署指定数据目录的媒体对象存储中；人工选择与恢复自动的接口见[导入与自动补缺](../metadata/metadata.md#导入歌词与自动补缺)。

歌手实体的 `identityKind=localCredit`：它表示“同一用户、同一 Provider 内的精确署名标签”，并非已验证的真实人物。两个 Provider 的相同署名拥有不同 ID；不会把它们与平台艺人实体合并。头像未知返回 null。

专辑搜索只返回已建立的显式 `SourceAlbum`，同名专辑仍保持独立身份。曲目标签的专辑名称可搜索，但不会仅凭标签同名自动合并成 `SourceAlbum`。专辑封面返回当前绑定的受鉴权媒体对象 ID；失效的目标版本或不可用对象不作为当前封面。

目录分类返回稳定哈希 ID、父 ID、根 ID、Provider ID 和末级目录名称；根显示为 `Root`，前端可用已授权 Provider 根名称替换。响应不返回绝对路径或相对路径串。

## 4. 固定集合、恢复和取消

每个页面带 `queryId`、规范化 `query`、`catalogRevision`、`expiresAt`、准确 `total`、`items` 与 `nextCursor`。快照同时固定曲库、专辑和收藏视图；创建后的扫描、元数据更新、收藏变更不会混入旧页。

快照有效期 10 分钟，读取不续期。游标绑定用户、查询 ID 和完整条件；不同用户、条件变更、跨查询游标、已取消或过期均不能继续原集合。游标失效返回 `CURSOR_EXPIRED`，前端须提示刷新；不能静默把新首批拼到旧结果。进程重启后仍可在剩余有效期内恢复。

“播放全部”应逐页收集固定结果中的稳定来源引用，直到 `nextCursor=null` 才算完整；缺失源仍在结果中保留其观测状态。实际开始播放时若源变更、插件下线或格式不支持，按播放接口失败跳过并汇总，不能把搜索命中视为已获播放权。用户取消时取消 HTTP 请求，并删除已创建查询释放服务端结果。

每用户最多 64 个活动查询，单次源记录上限 100,000，单快照最多 64 MiB，同用户保留结果最多 128 MiB；超出返回 `RATE_LIMITED`，不会截断后谎报完整。幂等键在快照过期后的 24 小时内保留，重复创建同条件返回原查询，修改条件返回 `CONFLICT`，过期键返回 `CURSOR_EXPIRED`。

## 5. 普通音乐范围验证

`tests/integration/storage/library_visibility_test.go` 与 `catalog_visibility_test.go` 覆盖音轨身份／文件位置关联、用户及来源隔离、混合 Recording、共用分类计数、专辑投影、筛选项、重新标记副本和固定快照。`tests/contract/api/work_package_catalog_http_test.go` 通过真实本地文件扫描与导入，验证列表排除、显式来源读取、跨用户拒绝以及作品音轨播放。

分类构造也受资源预算限制：读取目录路径最多 4096 字节、256 层子目录；路径、目录展开、专辑成员及分类索引纳入预算。目录按身份复用，面包屑只为实际返回的分类构造，最长包含根在内 257 项。超出预算返回 `RATE_LIMITED`（HTTP 429），不留下半成品查询；请求取消可中止分类构造。
