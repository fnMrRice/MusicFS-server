# 内容类型与作品资料

状态：2026-10-22。本文描述手工作品资料；文件关联通过显式[导入本地作品包](work-packages.md)完成，不因建档自动关联 SourceAlbum、Recording 或来源曲目。总体阶段和界面目标见[内容库设计](../../../docs/content-library.md)及[作品库页面](../../../docs/ui-works.md)。

## 内容类型

登录后 `GET /api/v1/content-types` 返回 `{items:[...]}`。每项包含 `id/pluginId/pluginVersion/name/description/creditRoles/externalIdNamespaces/views`。具有独立作品库的插件还声明 `library`，其中 `order` 决定曲目列表之后的入口顺序，`icon` 指定图标，`trackListOptional` 决定是否可按作品将音轨纳入普通曲目列表。

`sdk/contenttype` 定义声明、标识符与资源限制，`src/library/contenttypes` 管理只读注册表。内置插件在 `plugins/content` 定义并在 Server 启动时装配。调用方取得独立声明副本。

| 类型 | 默认作品库入口 | 进入普通曲目列表的条件 |
| --- | --- | --- |
| `music` | 既有普通音乐作品资料可按 ID 访问 | 普通音乐音轨 |
| `cd_album` | CD专辑，光盘图标 | 用户在该专辑开启 `showInTrackList` |
| `asmr` | ASMR | 仅在音声库浏览 |
| `radio` | 电台 | 仅在电台库浏览 |

CD 专辑沿用音乐署名及目录编号。ASMR 支持社团、声优、脚本、插画和 `dlsite` 编号。电台插件管理本地录音作品。署名和编号由用户录入。

当前固定支持 `work-list/work-detail`，不允许自定义脚本、HTML或动作URL。内置声明随 Server 构建，尚无动态安装、类型启停管理、任意自定义字段或复杂视图组合。既有通用 Provider/Scraper Manifest 不增加或复用角色。

## 作品接口

| 请求 | 行为 |
| --- | --- |
| `POST /api/v1/works` | 新建本人作品，校验已注册类型及其角色／编号命名空间 |
| `GET /api/v1/works/{id}` | 读取本人作品；其他用户返回404；类型不再注册仍可读 |
| `PUT /api/v1/works/{id}` | 按 `expectedRevision` 完整替换资料，不能更改内容类型 |
| `GET /api/v1/works` | 按本人范围检索，返回 `items/nextAfter` |

创建正文必须显式提供 `contentType/title/description/externalIds/credits/tags`；编辑用 `expectedRevision` 替代 `contentType`。空资料使用空字符串或空数组，不使用 null。修改要求允许的 Origin、CSRF 和8—128字符的幂等键。同用户、同操作、同键同参数重放原结果，不同参数409；窗口沿用内容操作的24小时。编辑修订不符返回409，不自动覆盖。

创建和完整替换均可传布尔值 `showInTrackList`，省略时取 `false`。仅声明 `library.trackListOptional` 的类型可请求 `true`。作品读取与列表均返回当前值。切换 CD 专辑展示后，新建的普通音乐查询使用新值，无需重扫或重新导入文件。

```json
{
  "contentType": "asmr",
  "title": "雨夜音声",
  "description": "手工录入的作品资料",
  "externalIds": [{"namespace": "dlsite", "value": "RJ123456"}],
  "credits": [{"role": "circle", "name": "示例社团"}, {"role": "voice_actor", "name": "示例声优"}],
  "tags": ["雨声"]
}
```

标题最多256字符，简介最多16384字符；外部编号最多32项，编号值最多256字符；署名最多128项，名称最多256字符；标签最多64项，每项最多64字符。字符串按规范去除首尾空白，简介允许换行及制表符，其他字段拒绝控制字符。角色、命名空间及类型使用小写 ASCII 标识符。相同编号允许出现在不同作品，不自动合并；单个作品的重复编号、署名和标签拒绝。

请求同时受普通控制接口的64 KiB总正文限制，各字段上限不意味着可以同时全部达到；超限返回明确请求错误，不截断保存。

返回 `workId/contentType/title/description/externalIds/credits/tags/revision/createdAt/updatedAt`，时间为 UTC。当前无删除、文件路径、封面或音轨字段。未知／停用类型可读，写入需要该类型仍注册；本批内置类型没有停用开关。

## 检索与分页

`contentType` 精确类型、`q` 字面子串搜索标题／简介／编号命名空间及值／署名名称、`tag` 精确标签、`credit` 精确署名名称，条件之间取交集。`q` 使用 SQLite 的 ASCII 大小写折叠，中文原样匹配，不承诺 Unicode 大小写或拼音检索。SQL 参数绑定，`%` 与 `_` 不解释为通配符。

各作品库按 `contentType=cd_album|asmr|radio` 分别查询，刚建档且没有副本的作品也能列出。可额外指定 `packageKind=directory|cd_dump` 筛选至少持有一个对应副本的作品，同类型的多个包仍只返回一次。副本标记在[作品包导入](work-packages.md)时选择。

`limit` 为1—100，默认50。按稳定作品编号升序，用响应 `nextAfter` 作为下一请求的 `after`；结束为 null。分页不是固定快照，创建或编辑可能改变后续结果；切换筛选必须清空旧分页并从头读取。无总数、创建时间排序或“播放全部作品”承诺。未知、重复、空查询参数及非法值返回400。

## 存储、迁移与验证

SQLite `026_works.sql` 新增作品、外部编号、署名与标签索引；业务合同归 `works.Repository`，SQLite 后端执行同一事务内的资料与索引发布。已纳入 `storage.Backend` 和生成的持久化 RPC 客户端；外部存储制品需配套构建。数据库备份继续覆盖整库，作品资料无需另设缓存文件。

针对性验收包括 SDK／注册表校验、作品重开持久化、账号隔离、并发修订、幂等、筛选分页、025→026迁移和HTTP／Schema校验。Web 提供手工资料入口，浏览器验证由 Web 仓库维护。

CUE 纯解析另见 [SDK说明](../../sdk/cuesheet/README.md)。它不读文件、不建档、也不创建播放会话；作品包导入中 CUE 仍作为附件，逻辑音轨持久化、音频区间验证和分段播放属于后续阶段。
