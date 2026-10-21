# 歌词与封面内容

歌词、封面提供独立文件存储、授权预览和人工选用。内容绑定 `sourceTrackId + assetId + assetVersion`，不会因 Recording 关联扩大权限。数据库保存格式、来源、摘要、引用和发布状态，正文保存在 `mediaRoot` 中，与音乐原件及播放缓存分开；接口不公开磁盘路径。

Media Store 在启动时打开并恢复，不依赖指纹或音频处理插件。上传和选用不写原件；[刮削与自动补缺](metadata.md)、[显式写回](writeback.md)使用独立流程。

## 上传与预览

登录后向 `POST /api/v1/media` 提交原始正文，以 query 指定 `sourceTrackId`、当前 `assetId`、`assetVersion`、`field=lyrics|artwork`，可选 `language`。请求需要 Cookie、允许的 Origin、`X-CSRF-Token` 和 8—128 字符的 `Idempotency-Key`。不接受客户端文件名、URL、来源实例或 `origin`；HTTP 上传来源固定为 `upload`。本地编辑后的文本也作为新对象上传，不原地修改已有对象。

上传成功返回 201 `ContentObject`，只创建可授权预览的对象，不改变当前选用内容。`GET /api/v1/media/{id}/info` 读取描述；`GET /api/v1/media/{id}` 返回实际正文。其他所有者与不存在统一返回 404；同一所有者仍可读取自己保留的旧资产对象。

| 内容 | 实际格式与限制 | 请求 Content-Type |
| --- | --- | --- |
| 歌词 | UTF-8，或带 BOM 的 UTF-16；校验后规范为 UTF-8。自动区分纯文本与 LRC，最多 1 MiB | `text/plain`，可带 charset 参数 |
| 封面 | 完整解码 PNG/JPEG；最多 8 MiB、1600 万像素，宽高各不超过 8192 | `image/png` / `image/jpeg` |

格式由实际字节校验，声明类型须与实际格式一致。歌词语言省略或空值统一为 `und`，其他语言标签统一小写；封面语言必须为空。所有选用、解锁和幂等操作使用同一语言规范，空值与 `und` 不能绕过同一人工锁。

上传有独立 8 MiB HTTP 正文预算与 30 秒读取/处理期限；普通控制请求仍为 64 KiB。媒体 GET 有 30 秒总交付及写入期限，完成后清理连接 deadline。正文响应使用真实 MIME、`nosniff` 和 `no-store`，不承诺 Range。超过 HTTP 预算为 400；格式、内容尺寸或解码不支持为 415。超时返回 503；已开始正文的交付失败会终止连接。

## 选用与解锁

先 `GET /api/v1/source-tracks/{id}/media-selection` 取得当前 `revision`。修订按所有者和来源隔离。随后以 Cookie、Origin、CSRF 和新的幂等键调用：

```http
PUT /api/v1/source-tracks/{id}/media-selection
Content-Type: application/json

{"objectId":"上传返回的编号","assetId":"当前资产编号","assetVersion":"当前版本","expectedRevision":0}
```

来源由路径确定，客户端不能提交 `automatic` 或伪造另一个来源。显式选用会为该字段/语言设置人工锁；封面与不同歌词语言分别占用槽位。数据库提交前再核验内容文件，已损坏或丢失的新预览不能覆盖原选择。旧修订、旧资产或失效资产不能成为新选择。

```http
POST /api/v1/source-tracks/{id}/media-selection/unlock
Content-Type: application/json

{"field":"lyrics","language":"und","expectedRevision":1}
```

解锁是独立动作，也要求幂等键；它只解除人工锁，不清空已有选择。上传、选用与解锁的幂等窗口为 24 小时，同键同规范化请求重放原结果，改变参数返回 409，过期键返回 410。上传按校验后的规范内容字节及规范元数据判定重放；例如带 BOM 的 UTF-16 歌词转为 UTF-8 后计算摘要，不按传入的原始编码字节判定。成功操作重放不会再次覆盖后续选择。

文件缺失或摘要不符时，正文读取返回 409 `RESOURCE_CHANGED` 并标记内容不可用，保留选择与人工锁。选中的当前资产内容可出现 `current=true,state=unavailable`；旧资产或已失效资产绑定为 `current=false`，可读旧对象为 `state=stale`。重新扫描不会把旧资产的内容悄悄声明为新资产内容，也不会清掉人工意图。

字段以 [OpenAPI](../contracts/openapi.yaml) 和 [HTTP Schema](../contracts/http.schema.json) 为准。

来源专辑和发行具有独立封面选择与人工锁，见[专辑流程](../library/albums.md)。派生对象使用同一个媒体读取入口。

## 派生内容

### 封面缩略图

对本人可读取且有效的 artwork 调用：

```http
POST /api/v1/media/{artworkObjectId}/thumbnail
Content-Type: application/json

{}
```

需要登录、Origin 和 CSRF。返回 `kind=thumbnail` 的派生对象，`metadata.transform=thumbnail256/v1`，标记原图片编号及 SHA-256。结果为保持比例、长边最多256像素的 PNG，小图不放大，保留透明度。输入沿用8 MiB、8192边长和1600万像素上限；完整解码后生成，最多两个并行生成。

同一所有者、原图片编号/摘要和转换版本复用同一有效对象，不要求幂等键。损坏对象先标不可用，再次生成可以建立新的对象；历史对象仍保留备份引用。原封面损坏时拒绝生成，不使用未经校验的字节。当前格式固定，不接受客户端提供路径、任意转换程序或尺寸。

### 完整刮削记录

每个持久刮削任务的每个目标执行代次保存一份 `scrape-document/v1` 文件，包括：

- 原请求、专辑编号/修订（适用时）、整个任务的成员及资产/选择修订快照、本次目标和执行代次。
- 资源的文档局部编号、版本、类型、大小、匹配依据和来源问题。
- 各实例的插件编号、真实握手版本、完整结构化搜索结果，包括被语言筛选或预算跳过的候选及所有问题。
- 逐候选取回/预览结果、失败码、未请求状态和已发布内容对象编号，以及本次收集结果与起止时间。

歌词和图片正文仍在它们各自的文件里。资源访问票据替换成文档局部编号，不记录工作进程通道 URL、本地路径或凭据。Schema 见 [scrape-document.schema.json](../contracts/scrape-document.schema.json)。记录是这次执行收集到的证据，最终任务和应用状态仍以任务 API 为准。

```http
GET /api/v1/metadata-jobs/{jobId}/documents
```

返回 `{items:[派生对象]}`，最多1024份；对象 `kind=scrape-document`，关系中有 job/source/asset/generation。仅列出已经完成文件引用发布的记录。取消或进程中断发生在发布前时，不伪造完整记录；重启后重新执行使用新代次，已发布历史记录保留。单份最大16 MiB，超限明确失败。

### 读取与恢复

`GET /api/v1/media/{objectId}/info` 返回描述；`GET /api/v1/media/{objectId}` 验证所有者及原来源归属后读取实际文件。缩略图使用 `image/png`，刮削文档使用 `application/json` 附件。没有磁盘路径，没有公开永久链接，不承诺 Range。原来源归属不再可用时，旧索引本身不能继续授予读取权限。

prepared/written/published 阶段未完成的派生发布在启动恢复时中止，不凭同名文件猜测完整；referenced 阶段校验文件后完成。损坏或缺失保留索引并明确不可用。当前、历史和不可用派生对象均进入备份清单，回收不会删除仍受引用保护的对象。

## 发布、回收与备份

### 发布与重启

文件写入前创建持久意图，按 `prepared → written → published → referenced → finished` 推进。staging 文件验证格式、长度与摘要并落盘后，才发布不可变对象，再提交数据库引用。人工选用另以预期修订事务提交，上传预览不会自动替换已选内容。

启动恢复先于后台工作：未建立引用的任务转为 `aborted`，移除其 staging；已发布未引用的孤儿保留到回收宽限期。已引用内容校验通过后完成日志，丢失或损坏则标不可用。同名文件不能证明写入成功。已选内容后续损坏时保留人工锁和历史，读取返回明确错误。

### 保守回收

启动恢复后回收一次，之后每小时一批，每批最多 128 项；默认宽限期 24 小时，内部配置不得低于一小时。只处理日志确认已放弃的对象编号，不枚举未知路径。删除前在事务内重查引用，再删除该编号的 staging/objects 文件并记录回执。中断后可重复执行；一个损坏项不会阻止同批其他项。

任何内容对象、指纹索引、活动发布或备份清单都保护文件；历史、未选预览、失效引用同样受保护。人工文件没有缓存淘汰策略。未知文件、目录和符号链接不由回收器删除；日志只报告脱敏错误码，异常项保留等待修复。

### 一致性备份组件

`mediastore.Backups.Create` 要求提供已存在的绝对父目录，在其中新建随机编号目录。失败后用新编号重做，保留旧目录证据，不覆盖已有路径。

先写持久意图，再通过所选后端的维护接口生成一致快照；当前 SQLite 使用 `VACUUM INTO`。引用清单从只读快照读取，检查 schema、完整性和外键，不能从更晚的在线库取清单。所有者、历史、预览和失效对象均记录并设置保护。最多 100000 个对象，分批复制时验证长度与 SHA-256；正文不进入数据库。

输出包括后端声明的快照文件（当前默认为 `db.sqlite`）、`media/objects` 和版本为 1 的 `manifest.json`。清单含数据库摘要、对象描述和 `copied`/`missing` 状态。不可读或损坏的原媒体记为缺失；复制中的其他错误或取消使本次返回失败。最终清单只在全部条目处理完并落盘后发布，没有它的目录不能视为完整备份。如果清单落盘后在线数据库完成回执失败，文件备份仍需独立核验，在线状态不能代替清单。

Linux 发布清单前同步媒体目录及父目录；Windows 使用同卷、不覆盖的 write-through 移动。不将本地磁盘行为推定为 NAS 保证。备份保护暂不自动释放，删除备份不自动清除在线 pin。

恢复将快照放入全新数据目录，再读取备份媒体。缺失的已选对象会转为 `unavailable`，保留人工锁；手工内容不一定能重新抓取。完整部署备份与恢复见[备份说明](../operations/backup.md)，不能只复制数据库。
