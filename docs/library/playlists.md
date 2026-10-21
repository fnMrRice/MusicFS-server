# 个人歌单：绑定、出处与隐藏

个人歌单支持本地追加、[远程歌单](remote-playlists.md)绑定、嵌套组合与独立隐藏意图。操作只改变 MusicFS 内的规则和快照，不修改远端平台或原始音频；聚合关系不授予来源访问权，也不保证曲目能播放。

## 有序绑定和稳定身份

登录后 `POST /api/v1/playlists` 创建歌单，必需name和bindings。名称去两端空白后1—256个Unicode字符，不作为身份。bindings有序，每项kind为以下之一：

| kind | 输入 | 含义 |
| --- | --- | --- |
| local | entries数组，每项source | 用户明确追加的来源引用，可为本人sourceTrackId或已导入的remoteSourceId，恰好一种 |
| remote | targetId为本人已显式导入的远程歌单ID，entries空 | 引用其当前本地快照；不会自动访问远端 |
| playlist | targetId为本人另一个个人歌单ID，entries空 | 引用该歌单构建后的固定子快照 |

例如创建一个手动列表：

```json
{"name":"我的歌单","bindings":[{"kind":"local","entries":[{"source":{"sourceTrackId":"已有来源编号"}}]}]}
```

新bindingId/entryId省略或空字符串，由宿主生成；编辑既有绑定或出现时必须保留其编号。可重排、移除已有项或插入新项，但不能保留同一编号却更换kind、目标歌单或来源。重复曲目保留独立出现身份。`PUT /playlists/{id}` 必填 `expectedRevision/name/bindings`，整体替换有序规则；旧修订与非法身份重用返回409，未知或其他用户来源404，循环拒绝。

每个歌单最多128个绑定、显式追加总数最多10000；图最多128个个人歌单、深度16，展开贡献最多20000。HTTP仍采用既有64 KiB控制请求上限，没有因为理论条目上限放宽上传预算。普通创建、编辑、重建及隐藏修改都要求Cookie、允许Origin、CSRF和8—128字符幂等键，24小时内同键同规范参数重放原结果，异参数冲突。

## 三种修订与固定分页

`ruleRevision` 从1开始，记录名称和绑定修改；`hideRevision` 从0开始，记录隐藏意图修改；`snapshotRevision` 从0开始，指向最后构建结果。三者相互独立。`GET /playlists/{id}` 和 `GET /playlists?after=…&limit=50` 返回规则及stale状态，limit为1—100。列表不固定跨页快照；子快照、远程本地快照、匹配或隐藏变化会使相关结果需要重建。

`POST /playlists/{id}/rebuild {"expectedRuleRevision":1}` 在事务内固定规则、隐藏、当前人工匹配修订和子快照依赖。返回快照收据：playlistId、revision、ruleRevision、hideRevision、matchingRevision、dependencies、needsConfirmation、createdAt；不一次展开所有贡献。有效输入未变时可复用原不可变快照，同键重放仍返回原收据。

`GET /playlists/{id}/snapshots/{revision}?offset=0&limit=50` 按固定本地修订分页，只返回至少一个贡献未隐藏的条目。offset是可见条目序号，position保留原聚合位置，隐藏后可能不连续；nextOffset为空表示结束。每次读取仍检查来源所有权，持有历史编号不绕过权限。旧快照不会因后来的规则、匹配或隐藏修改而重写。

聚合按binding顺序和原出现顺序展开。已经由人工确认同Recording的本地来源合成一个条目；未关联本地来源按sourceTrackId，远程来源按remoteSourceId去重。元数据相似、指纹候选或同名本身不产生关联。去重条目保留所有provenance：完整bindingPath、原歌单编号、原entryId、SourceRef、present/suspectedRemoved及hidden标记。当前可见来源用于条目source，所有出处仍保留。删除一个binding只移除它的贡献，不删除其他出处或源资产。

## 隐藏意图与确认

`GET /playlists/{id}/hidden-rules` 返回独立revision与规则。`POST` 同一路径必填 `expectedRevision/source`，scope省略时为allSources；可选范围如下：

- allSources：隐藏当前已知关联成员的所有出处，不传bindingPath/entryId。
- binding：提供完整bindingPath，限制该绑定及其下游贡献，不传entryId。
- entry：同时提供完整bindingPath与原entryId，仅限制一次实际出现。

SourceRef始终保存原始来源锚点，不能只交Recording编号。锚点及指定出处必须在当前组合中实际存在；全曲隐藏不会删除来源，也不会更改播放权限。只要去重条目仍有一个未隐藏的贡献，它仍可见。

创建隐藏规则时固定当时允许的关联成员集合。之后人工关联扩大范围，新成员仍保持可见，规则和快照标记needsConfirmation。调用 `PUT /playlists/{id}/hidden-rules/{ruleId}`，显式提供 `expectedRevision/active/confirm`；只有active=true且confirm=true才确认当前扩大范围。active=false、confirm=false解除该规则的效果但保留原意图记录；confirm=true与active=false矛盾，拒绝。

解除错误关联或拆分后，隐藏继续追随原始SourceRef所在的结果，不能扩散到所有拆分结果。移除某个绑定不删除其隐藏规则，也不会把它转成全局范围；后续重建继续按原路径解释。每个歌单最多1024条隐藏规则，用户应根据最新hideRevision操作。

`GET /playlists/{id}/changes?after=0&limit=50` 返回create/edit/hide/changeHidden审计，记录ruleRevision、hideRevision与可空ruleId。after为上页nextAfter十进制序号，limit1—100；重建快照与人工规则变更分开，不伪造远端写回记录。

## 运行边界

不增加新的YAML开关。本地引用沿用Provider授权，远程引用必须先[显式导入与同步](remote-playlists.md)；聚合重建不会联网同步。禁用在线实例后，已有本地远程快照和本人聚合历史仍可读取。原始WAV播放、Range与停止沿用既有播放接口，隐藏只是歌单展示规则，不是禁止播放来源。

公开模型见 [OpenAPI](../contracts/openapi.yaml)与 [HTTP Schema](../contracts/http.schema.json)。平台账号控制面已接入，真实授权仍待验收；私人平台歌单、远端编辑和完整音频解析尚未支持，见[账号登录](../providers/platform-login.md)。
