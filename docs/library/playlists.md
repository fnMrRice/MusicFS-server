# 个人歌单：绑定、出处与隐藏

个人歌单支持本地追加、[远程快照](remote-playlists.md)绑定、嵌套组合与独立隐藏意图。用户还可单独关联一个可写的网易云歌单，将此后新增的网易云来源曲目追加到远程。聚合规则、隐藏和排序在 MusicFS 内生效。[账号歌单与关联](source-playlists.md)

## 有序绑定和稳定身份

登录后 `POST /api/v1/playlists` 创建歌单，必需name和bindings。名称去两端空白后1—256个Unicode字符，不作为身份。bindings有序，每项kind为以下之一：

| kind | 输入 | 含义 |
| --- | --- | --- |
| local | entries 数组，每项带 source | 本人曲目、显式来源或已导入的远程条目。曲目可以没有播放来源 |
| remote | targetId为本人已显式导入的远程歌单ID，entries空 | 引用其当前本地快照；不会自动访问远端 |
| playlist | targetId为本人另一个个人歌单ID，entries空 | 引用该歌单构建后的固定子快照 |

例如创建一个手动列表：

```json
{"name":"我的歌单","bindings":[{"kind":"local","entries":[{"source":{"trackId":"曲目编号"}},{"source":{"trackId":"另一首曲目编号","forcedSourceTrackId":"指定的来源音轨编号"}}]}]}
```

省略新绑定或新条目的编号时由宿主生成。编辑时保留已有的 bindingId、entryId 和 trackId，可以调整顺序、更改该条目的强制来源或恢复自动选择。`PUT /playlists/{id}` 使用 `expectedRevision/name/bindings` 整体替换有序规则，旧修订返回 409。指定来源必须属于本人及该曲目。两个不同的指定版本可以分别出现在同一歌单中。

曲目引用通过 `music_playlist_entries` 保存。未指定来源时，播放时读取曲目的当前默认来源；更改默认来源无需重建歌单。指定的来源缺失或已移出该曲目时，该条目仍保留，只能在更改选择或恢复来源后播放。无来源曲目同样可以加入歌单。已有的本地歌单条目在数据库升级时按原先选定的来源转换。远程平台的条目继续保存原远程标识。

每个歌单最多128个绑定、显式追加总数最多10000；图最多128个个人歌单、深度16，展开贡献最多20000。HTTP仍采用既有64 KiB控制请求上限，没有因为理论条目上限放宽上传预算。普通创建、编辑、重建及隐藏修改都要求Cookie、允许Origin、CSRF和8—128字符幂等键，24小时内同键同规范参数重放原结果，异参数冲突。

## 三种修订与固定分页

`ruleRevision` 从1开始，记录名称和绑定修改；`hideRevision` 从0开始，记录隐藏意图修改；`snapshotRevision` 从0开始，指向最后构建结果。三者相互独立。`GET /playlists/{id}` 和 `GET /playlists?after=…&limit=50` 返回规则及stale状态，limit为1—100。列表不固定跨页快照；子快照、远程本地快照、匹配或隐藏变化会使相关结果需要重建。

`POST /playlists/{id}/rebuild {"expectedRuleRevision":1}` 在事务内固定规则、隐藏、当前人工匹配修订和子快照依赖。返回快照收据：playlistId、revision、ruleRevision、hideRevision、matchingRevision、dependencies、needsConfirmation、createdAt；不一次展开所有贡献。有效输入未变时可复用原不可变快照，同键重放仍返回原收据。

`GET /playlists/{id}/snapshots/{revision}?offset=0&limit=50` 按固定本地修订分页，只返回至少一个贡献未隐藏的条目。offset是可见条目序号，position保留原聚合位置，隐藏后可能不连续；nextOffset为空表示结束。每次读取仍检查来源所有权，持有历史编号不绕过权限。旧快照不会因后来的规则、匹配或隐藏修改而重写。

聚合按绑定顺序和原出现顺序展开。使用 trackId 的条目按曲目与强制来源组合去重，两个不同的指定版本保留为两项。以 sourceTrackId 显式贡献来源时使用下列聚合规则。已经由人工确认同Recording的本地来源合成一个条目；未关联本地来源按sourceTrackId，远程来源按remoteSourceId去重。元数据相似、指纹候选或同名本身不产生关联。去重条目保留所有provenance：完整bindingPath、原歌单编号、原entryId、SourceRef、present/suspectedRemoved及hidden标记。当前可见来源用于条目source，所有出处仍保留。删除一个binding只移除它的贡献，不删除其他出处或源资产。

显式来源聚合条目有多个不同且未隐藏的本地来源时，重建在这些来源中选择默认版本：先采用该录音的手动默认，否则优先可用来源，再按码率从高到低、相同码率时按原文件大小从大到小选择。未知码率或大小排在已知正值之后，完全相同时按显示名和来源 ID 保持稳定。手动默认即使暂时不可用仍保留；若它不在歌单贡献中或贡献均已隐藏，则从其余可见来源自动选择，不引入歌单外来源或恢复已隐藏贡献。明确单独追加一个来源时仍使用该来源。

重新生成快照会读取当前码率、文件大小和可用性；即使人工关联修订未变化，默认来源发生变化也会发布新快照，并更新嵌套歌单依赖。来源选择及其他有效输入均未变化时复用原快照。显式来源聚合快照固定重建时的选源。普通曲目条目保存 trackId 和可选强制来源，每次播放时再读取当前可用版本。

## 隐藏意图与确认

`GET /playlists/{id}/hidden-rules` 返回独立revision与规则。`POST` 同一路径必填 `expectedRevision/source`，scope省略时为allSources；可选范围如下：

- allSources：隐藏当前已知关联成员的所有出处，不传bindingPath/entryId。
- binding：提供完整bindingPath，限制该绑定及其下游贡献，不传entryId。
- entry：同时提供完整bindingPath与原entryId，仅限制一次实际出现。

隐藏普通曲目时提交条目中的 trackId 和可选 forcedSourceTrackId。隐藏显式贡献时提交对应的来源编号。锚点及指定出处必须在当前组合中实际存在；全曲隐藏不会删除来源，也不会更改播放权限。只要去重条目仍有一个未隐藏的贡献，它仍可见。

创建隐藏规则时固定当时允许的关联成员集合。之后人工关联扩大范围，新成员仍保持可见，规则和快照标记needsConfirmation。调用 `PUT /playlists/{id}/hidden-rules/{ruleId}`，显式提供 `expectedRevision/active/confirm`；只有active=true且confirm=true才确认当前扩大范围。active=false、confirm=false解除该规则的效果但保留原意图记录；confirm=true与active=false矛盾，拒绝。

解除错误关联或拆分后，隐藏继续追随原始SourceRef所在的结果，不能扩散到所有拆分结果。移除某个绑定不删除其隐藏规则，也不会把它转成全局范围；后续重建继续按原路径解释。每个歌单最多1024条隐藏规则，用户应根据最新hideRevision操作。

`GET /playlists/{id}/changes?after=0&limit=50` 返回create/edit/hide/changeHidden审计，记录ruleRevision、hideRevision与可空ruleId。after为上页nextAfter十进制序号，limit1—100；重建快照与人工规则变更分开，不伪造远端写回记录。

## 运行边界

不增加新的YAML开关。本地引用沿用Provider授权，远程引用必须先[显式导入与同步](remote-playlists.md)；聚合重建不会联网同步。禁用在线实例后，已有本地远程快照和本人聚合历史仍可读取。原始WAV播放、Range与停止沿用既有播放接口，隐藏只是歌单展示规则，不是禁止播放来源。

公开模型见 [OpenAPI](../contracts/openapi.yaml)与 [HTTP Schema](../contracts/http.schema.json)。网易云关联歌单支持追加歌曲，真实账号授权和写入的验收范围见[账号登录](../providers/platform-login.md)。
