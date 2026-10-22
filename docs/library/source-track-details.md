# 来源曲目详情

重复音乐分组展开后，每个来源版本可通过 `GET /api/v1/source-tracks/{id}/details` 读取详情。响应中的 `sourceTrackId`、`providerInstanceId` 由宿主绑定到当前用户的来源版本；`externalId`、`title`、`sections` 来自该来源插件。

`sections` 是有序分组，每组包含 `id`、`title` 和有序 `fields`，字段包含 `id`、`label`、`value`。这些标题、标签和值全部是插件提供的纯文本，不包含可执行 HTML、客户端组件或平台专属渲染规则。客户端保留分组顺序并按文本展示；链接文本不自动执行。空 `sections` 表示插件没有附加信息。

插件 SDK 使用 `pluginproto.TrackDetails`。本地控制面 `provider.trackDetails` 接收 `ResolveRequest`，必须绑定来源曲目标识与预期扫描版本；在线控制面 `provider.online.trackDetails` 接收 `OnlineReference` 并返回带作用域的 `OnlineResult[TrackDetails]`。插件通过 `track.details` 能力声明支持，未实现时返回 `UNSUPPORTED`，不回退到宿主数据库字段。

最多允许 16 个分组，每组最多 64 个字段，总计最多 128 个字段；分组标识必须唯一，组内字段标识必须唯一。字段值最多 16384 个字符，整个插件结果最多 128 KiB。标题和标签不允许控制字符，字段值仅允许换行和制表符。宿主校验来源身份、响应结构和这些限制。

本地插件展示自己持有的文件观察信息：文件名、来源内相对路径、来源目录标识、大小、媒体类型、来源曲目标识和扫描版本，以及存在时的编码、时长、码率、来源声明摘要。详情不暴露服务器绝对路径。宿主在调用前后检查文件和扫描版本；文件被删除、替换或修改时明确失败，需重新扫描。音频标签由解码流程产生，不作为本地来源插件自行提供的信息填入详情。

在线接口 `GET /api/v1/online-instances/{id}/tracks/{externalId}/details` 返回同一插件结构。宿主校验实例所有者、插件状态、平台登录，并在调用后复核账户修订。当前网易云插件提供歌曲和来源分组；QQ 音乐、Apple Music 尚未验证的能力保持 `UNSUPPORTED`。详情不改变曲目、合并关系、原文件或播放授权。
