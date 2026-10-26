# 曲目列表全选与批量操作

动态加载的曲目列表使用 `TrackSelection` 保存选择：普通选择传 `batch:false` 和 `sourceTrackIds`；全选传 `batch:true`，将单独取消的代表行 ID 放入 `exclude`。全选覆盖当前账号普通音乐范围内的全部匹配行，与浏览器已加载页数无关。来源筛选用 `providerInstanceId`，合并版本视图用 `groupBy:"recording"`。

全选必须传列表的 `libraryRevision`；合并版本视图还必须传 `matchingRevision`。服务端在执行操作的同一事务中校验修订并解析选择，变更后返回 `CONFLICT`，客户端重新加载并重新选择。范围外或其他账号的显式 ID、排除 ID 返回 `NOT_FOUND`。分组视图排除代表行会排除整组。

- `POST /me/favorites/batch` 接收 `{selection,favorite}`，原子更新代表曲目的收藏；预先校验 20000 条收藏限额，保留已有收藏时间。返回实际变化数 `affected` 和所有所选代表 `sourceTrackIds`，便于直接更新客户端状态。
- `POST /playlists/selection` 接收 `{selection,name}`，创建包含所选代表曲目的 local 绑定；`POST /playlists/{id}/selection` 接收 `{selection,expectedRevision}`，追加绑定。两者要求 `Idempotency-Key`，返回 `{playlistId,ruleRevision,count}`，不读取或返回完整歌单。仍执行 128 个绑定、10000 条本地引用及现有图约束。
- `POST /matching/links` 可接收 `selection`，与原有 `sourceTrackIds` 互斥。关联操作展开所选分组的全部来源版本，保留 256 来源上限、人工排除、修订与幂等校验。
- `POST /source-tracks/selection` 直接接收选择，仅为播放队列返回紧凑曲目引用；收藏、歌单和关联操作直接发送选择，不提前读取完整曲目列表。

批量写入失败时回滚整个事务，不留下部分收藏、歌单引用或关联。收藏单次选择最多 20000 行，歌单单次和最终本地引用总数最多 10000 条；超出限额应提示用户缩小选择范围。
