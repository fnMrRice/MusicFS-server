# 在线资料刮削插件

打开作品详情中的「搜索在线资料」，或从曲目入口搜索歌词、封面。选择内容和来源，核对候选的版本、来源页面及预览后选用。作品资料按字段保存，并保留来源编号；后续编辑作品标题或简介会保留已选曲目表和图像。选用来源曲目的歌词或封面会发布到媒体对象库，并通过现有媒体修订与人工锁生效。

「曲目列表 → 专辑」打开具体专辑后，也可使用「搜索在线资料」查询专辑详情、专辑曲目和专辑封面。选用结果按当前音乐专辑和字段独立保存，刷新或重启后仍可读取；每次选用校验并递增专辑修订，同时更新曲库修订。已有专辑名称、手填资料、曲目标签与关联保留；刮削曲目表作为资料预览，不自动创建、删除或重排曲库曲目。「未知专辑」没有独立专辑身份，不提供此入口。

| 插件 | 声明的内容 | 查询方式 |
| --- | --- | --- |
| MusicBrainz | 曲目详情、专辑详情、专辑曲目、歌手详情 | 名称、对应实体的 MBID，或录音的 ISRC。[接口](https://musicbrainz.org/doc/MusicBrainz_API) |
| Cover Art Archive | 专辑封面 | 具体发行版 MBID，或用专辑名找到对应版本。[接口](https://musicbrainz.org/doc/Cover_Art_Archive/API) |
| LRCLIB | 歌词 | 歌名、艺人、专辑、时长，或歌词记录编号；返回同步 LRC 或纯文本。[接口](https://lrclib.net/docs) |
| iTunes Search | 曲目详情、专辑详情、专辑曲目、歌手详情 | 名称或商店编号。地区使用两位国家或地区代码，例如 HK、TW。仅取文字资料。[接口](https://performance-partners.apple.com/search-api) |
| Bangumi | 作品及专辑详情、专辑曲目、封面、歌手详情和歌手图像 | 搜音乐条目或人物，或提供相应的条目／人物编号。图像取自条目封面或人物头像。[接口](https://bangumi.github.io/api/) |
| DLsite | 作品详情、封面 | 用 RJ 作品编号精确查询，使用商店页面自身的 JSON 接口。字段和访问能力可能随商店变更。 |
| AcoustID | 录音识别 | 提供整曲 Chromaprint 指纹与时长（秒），返回候选 MusicBrainz 录音编号。部署时在 Server 进程设置环境变量 `MUSICFS_ACOUSTID_API_KEY`，重启后启用。免费服务适用非商业使用。[接口与注册](https://acoustid.org/webservice) |

作品以来源链接保存图片，预览时由浏览器向相应站点加载。对曲目选用封面时，Server 按图片主机声明验证下载和重定向，再经媒体服务校验图片字节。每个字段展示的候选取决于上游实际收录。在线查询在用户主动操作时发起，本地目录扫描保持原有导入策略。

## 插件与 HTTP 契约

插件实现 `sdk/scraper.Plugin`，提供 `Descriptor` 和 `Search`。声明包括 `type=scraper`、稳定的插件编号、支持的 `fields` 和配置状态。图片来源通过 `imageHosts` 显式声明。插件管理按 `scraper` 用途列出，内置能力直接取插件声明。

- `GET /api/v1/scrapers` 列出在线插件及其字段。本地提取实例按原有 `GET /api/v1/scraper-instances` 查询。
- `POST /api/v1/scrapers/{id}/search` 接受一个字段和结构化查询，查询结果包括来源信息及短期候选令牌。预览绑定当前用户、插件和字段，过期或 Server 重启后可重新搜索。
- `PUT /api/v1/works/{id}/scraped-fields/{field}` 人工选用作品的一个字段。请求提供 `pluginId`、`candidateToken`、`expectedRevision` 和幂等键。用 `GET /api/v1/works/{id}/scraped-fields` 读取已保存的资料。
- `GET /api/v1/library/albums/{id}/scraped-fields` 读取当前用户音乐专辑已选用的资料；`PUT /api/v1/library/albums/{id}/scraped-fields/{field}` 按音乐专辑修订选用 `albumDetails`、`albumTracks` 或 `artwork`，请求提供 `pluginId`、`candidateToken`、`expectedRevision` 和幂等键，成功返回新的 `revision`。重放相同成功请求不依赖短期候选仍然有效。
- `PUT /api/v1/source-tracks/{id}/scraped-fields/{field}` 支持 `lyrics` 和 `artwork`。请求还需提供 `assetId` 与 `assetVersion`；修订从该来源曲目的媒体选择接口读取。成功后通过现有媒体接口读取正文与来源。
