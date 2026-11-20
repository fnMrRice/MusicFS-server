# 在线资料刮削插件

打开作品详情中的「搜索在线资料」，或从曲目入口搜索歌词、封面。选择内容和来源，核对候选的版本、来源页面及预览后选用。作品资料按字段保存，并保留来源编号；后续编辑作品标题或简介会保留已选曲目表和图像。选用来源曲目的歌词或封面会发布到媒体对象库，并通过现有媒体修订与人工锁生效。

「曲目列表 → 专辑」打开具体专辑后，专辑名右侧的「详情」展开已保存资料和相应平台页面；播放工具栏右侧的「刮削」查询专辑详情、完整曲目表和封面。先选择发行版，再以同一来源编号读取全部曲目，预览后一并保存；上游未返回完整曲目时不发布半份资料。MusicBrainz、iTunes 与 Bangumi 支持填写相应专辑页面，插件只解析本平台的 HTTPS 地址及实体编号，不把任意网址当作抓取地址。「未知专辑」没有独立专辑身份，不提供此入口。

选用结果按当前音乐专辑和字段保存，并在同一事务补齐专辑曲目：优先复用专辑内唯一精确匹配的已有曲目，缺少音源的曲目建为资料条目，以灰色显示；按平台碟序和曲序排列。相同发行版重刮按位置复用身份，保留人工编辑及真实来源，不重复建档。原来额外关联的曲目继续保留，排在平台曲目之后。「全部曲目」仍使用有来源过滤。保存校验专辑修订并支持幂等重试，刷新、重启后仍可读取。

专辑封面读取优先采用已选封面、专辑详情图像，再回退专辑原封面、普通曲目和当前可见来源封面。返回受鉴权媒体地址或有效 HTTPS 图片地址，不把文件系统路径当作网页图片。专辑名可用原有编辑接口手动修改；只读的 `originalName` 始终保留首次名称。

专辑对应的平台页面也可在编辑资料时修改。`sourceUrl` 和 `sourcePluginId` 必须配对，页面只允许无凭据的 HTTPS 地址；保存关联不会查询平台或覆盖资料。已有专辑详情在数据库升级时回填关联，新选用完整发行版时自动记录其页面。用户点击刷新后才访问对应插件的固定平台 API，读取完整详情与曲目并展示比较；取消比较不修改资料。

对比合并支持名称、歌手、发行日期、发行商、音乐风格、说明、封面和曲目表。只发布明确勾选的字段，未选的已保存详情也保持；部分日期保留年份／年月精度，完整日期才写主发行日期。选择封面时替换旧专辑封面选用，选择曲目表时才同步实际曲目与顺序。名称修改不改变原名，合并不修改用户已保存的关联页面。详情的来源编号与页面记录最近一次合并平台，保留下来的人工或旧字段不会因此被覆盖，也不表示这些字段全部来自最近平台。专辑修订保护关联变更后的旧比较，保存回执支持候选失效后的幂等重放。

| 插件 | 声明的内容 | 查询方式 |
| --- | --- | --- |
| MusicBrainz | 曲目详情、专辑详情、专辑曲目、歌手详情 | 名称、对应实体的 MBID，或录音的 ISRC。[接口](https://musicbrainz.org/doc/MusicBrainz_API) |
| Cover Art Archive | 专辑封面 | 具体发行版 MBID，或用专辑名找到对应版本。[接口](https://musicbrainz.org/doc/Cover_Art_Archive/API) |
| LRCLIB | 歌词 | 歌名、艺人、专辑、时长，或歌词记录编号；返回同步 LRC 或纯文本。[接口](https://lrclib.net/docs) |
| iTunes Search | 曲目详情、专辑详情、完整专辑曲目、歌手详情及接口提供的封面 | 名称、商店编号或专辑页面。地区使用两位国家或地区代码，例如 HK、TW；页面地区可直接用于查询。封面采用接口实际返回的地址，不猜测放大地址。[接口](https://performance-partners.apple.com/search-api) |
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
- `PUT /api/v1/library/albums/{id}/scraped-fields` 原子选用详情和全部曲目，请求为 `{expectedRevision,pluginId,candidates:{albumDetails,albumTracks,artwork?}}`，候选值均为短期令牌；要求同插件、同发行版，且曲目表非空。成功后统一递增专辑与曲库修订并返回 `{revision}`。单字段接口仍可独立选封面。
- `PUT /api/v1/library/albums/{id}/metadata-merge` 请求为 `{expectedRevision,pluginId,candidates:{albumDetails,albumTracks},fields:[...]}`，完整两候选仍须同插件及发行版；`fields` 为非空去重白名单：`name`、`artist`、`releaseDate`、`publisher`、`genre`、`description`、`cover`、`tracks`。选中的可空值缺失时清空该字段；名称不能为空。预览和合并分别进行，保存成功返回新的 `{revision}`。
- `PUT /api/v1/source-tracks/{id}/scraped-fields/{field}` 支持 `lyrics` 和 `artwork`。请求还需提供 `assetId` 与 `assetVersion`；修订从该来源曲目的媒体选择接口读取。成功后通过现有媒体接口读取正文与来源。
