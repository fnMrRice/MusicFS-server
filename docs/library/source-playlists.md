# 按音乐来源读取账号歌单

设置页统一使用“音乐来源”。插件以 `features: ["music-source"]` 声明来源功能，以 `playlist.list` 声明是否支持读取当前账号的歌单；来源名称和译名遵循 [插件身份与名称](../plugins/plugin-identity.md)。两种声明分别回答“是什么来源”和“能做什么”，不能因为它是音乐来源就假定一定支持歌单。

## 接口与显示

`GET /api/v1/online-instances/{id}/playlists?limit=20&cursor=...` 调用对应插件的 `provider.online.playlists`，返回：

```json
{
  "items": [],
  "nextCursor": null,
  "consistency": "unversioned",
  "observedAt": "2026-10-10T00:00:00Z"
}
```

`items` 使用 `OnlinePlaylist` 结构，包含平台歌单 ID、标题、资料链接及可选曲目数、封面和创建者。`writable: true` 表示当前账号可以向该歌单添加歌曲。网易云插件根据平台的创建者 ID 判断所有权。`limit` 默认 20，范围 1—50，游标最长 1024 字符。读取使用本实例当前已校验账号。

歌单管理和左侧导航直接显示“网易云音乐-歌单名”。歌单按实例分组，点击歌单可查看曲目。两个入口共享账号歌单的分页结果，用户可加载更多歌单。

网易云会在首页额外返回置顶的“我喜欢的音乐”。插件每次向网易云请求 50 张，在每个上游批次内按界面请求的条数分页。续页会重新核对上一页末尾的歌单 ID。进入下一批时，上游偏移只计算普通歌单。

分页游标绑定用户、实例、查询类型、页大小、平台账号和登录代次，不能用于搜索、其他账号或新的登录。`/online-instances` 在账号已校验时返回可选 `accountRevision`，Web 将其纳入列表缓存身份；其他标签页退出后重新登录同一账号，也会在下一次来源刷新时使旧列表、游标和在途结果失效。当前没有平台账号变更推送，不能在未刷新时立即感知另一标签页的变化。当前页退出平台会立即取消在途读取并清理该来源分类；退出 MusicFS 或鉴权失效清理所有来源歌单缓存，迟到响应不能恢复旧分类。

## 在歌单管理中关联

登录后的网易云账号歌单也出现在歌单管理中。在可写的远程歌单上选择本地歌单即可建立关联。每张远程歌单最多关联一张本地歌单。之后向该本地歌单添加网易云来源的歌曲时，服务端使用关联账号将该歌曲添加到远程歌单。远端已经有该歌曲时视为同步成功。

服务端的 `GET /playlist-links` 查询已有的关联。`PUT` 和 `DELETE /online-instances/{id}/playlists/{externalId}/link` 分别用于关联和解除关联。向本地歌单添加在线浏览页面中的歌曲前，通过 `POST /online-instances/{id}/playlist-track-sources` 登记来源。请求和响应结构见 [OpenAPI](../contracts/openapi.yaml)。这些路径均以 `/api/v1` 为前缀。

## 当前范围与依据

- 网易云插件新增本人创建/收藏歌单的账号列表，固定请求 `/api/user/playlist`，`uid` 取自插件内已校验账号。参数依据 [api-enhanced 固定版本的 user_playlist.js](https://github.com/NeteaseCloudMusicApiEnhanced/api-enhanced/blob/2db6684453297ee86dec0492842f2320b08182d9/module/user_playlist.js)，请求复用当前已接入的账号 EAPI 通道。分页顺序来自上游，不声明稳定快照；总量上限 10000，超限明确报错，不伪装完整列表。
- QQ 音乐、Apple Music 及 Local Files 当前未提供 `playlist.list`，不会显示空歌单分类。后续插件提供能力与非空列表后，Web 无需新增平台名称映射。
- 网易云支持读取公开歌单和当前账号自己的私密歌单。收藏的其他人公开歌单可浏览，关联写入限本人歌单。无法验证归属的私密歌单会返回访问错误。
- 支持 `playlist.addTracks` 的网易云插件会在写入前检查当前账号、目标归属及成员。请求固定发送到 `/api/playlist/manipulate/tracks`，沿用登录时的账号通道。参数和平台 512 响应的处理依据 [api-enhanced 固定版本的 playlist_tracks.js](https://github.com/NeteaseCloudMusicApiEnhanced/api-enhanced/blob/2db6684453297ee86dec0492842f2320b08182d9/module/playlist_tracks.js)。
- 账号歌单分页的回归测试覆盖置顶、跨批次和游标失效。向网易云歌单写入曲目的实号验收范围见[账号登录](../providers/platform-login.md#未验收范围)。
