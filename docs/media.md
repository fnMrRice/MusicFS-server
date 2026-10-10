# 歌词与封面内容

S-P6-01 提供歌词、封面的独立文件存储、授权预览和人工选用接口。内容绑定 `sourceTrackId + assetId + assetVersion`，不会因为 Recording 关联而扩大权限。SQLite 保存格式、来源、摘要、引用和发布状态，正文保存在配置的 `mediaRoot` 中；接口不公开磁盘路径。

当前接口无需启用指纹或音频处理插件，启动时始终打开并恢复 MediaStore。本步骤没有 Web 编辑界面、Scraper 搜索流程或写回源文件；自动补缺、删除回收和备份属于后续步骤，不能把本节接口视为已完成这些流程。

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

真实 HTTP、独立服务重启和播放检查见[验收记录](validation/media-http.md)；字段以 [OpenAPI](contracts/openapi.yaml) 和 [HTTP Schema](contracts/http.schema.json) 为准。

来源专辑和发行具有独立封面选择与人工锁，见[专辑流程](albums.md)。已存封面可生成保持比例的256像素 PNG 缩略图；完整刮削记录也作为独立文件保留，见[派生媒体](derived-media.md)。这两类对象通过同一个媒体读取入口授权交付。
