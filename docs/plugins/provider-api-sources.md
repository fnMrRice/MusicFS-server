# 刮削与在线音乐源的接口来源

刮削插件为已有曲库查找资料。在线音乐源插件负责访问平台账号、曲目或歌单。网易云音乐和 QQ 音乐各有两种插件，它们使用的接口和登录条件不同。

## 资料刮削

| 插件 | MusicFS 读取的内容 | 接口文档或参考来源 |
| --- | --- | --- |
| MusicBrainz | 通过 `/ws/2/` 按名称、MBID 或 ISRC 查录音、发行版和艺人 | [MusicBrainz API](https://musicbrainz.org/doc/MusicBrainz_API) |
| Cover Art Archive | 根据 MusicBrainz 发行版 MBID 读取正面封面，使用 `/release/{mbid}/front-500` | [Cover Art Archive API](https://musicbrainz.org/doc/Cover_Art_Archive/API) |
| LRCLIB | 通过 `/api/get`、`/api/get/{id}`、`/api/search` 查同步歌词或纯文本歌词 | [LRCLIB API](https://lrclib.net/docs) |
| iTunes Search | 使用 `search` 和 `lookup` 按商店查询曲目、专辑、歌手和封面 | [Apple 的 iTunes Search API 文档（归档）](https://developer.apple.com/library/archive/documentation/AudioVideo/Conceptual/iTuneSearchAPI/index.html) |
| Bangumi | 从 v0 的 `subjects`、`persons` 和 `episodes` 读取音乐条目、人物、曲目表及图像 | [Bangumi API](https://bangumi.github.io/api/) · [OpenAPI 源文件](https://github.com/bangumi/api) |
| AcoustID | 向 `/v2/lookup` 提交 Chromaprint 指纹及音频时长，取得候选 MusicBrainz 录音编号。部署时需要 AcoustID 应用密钥 | [AcoustID Web Service](https://acoustid.org/webservice) |
| 网易云音乐 | 使用网页目录的搜索、歌曲详情、歌词、专辑及歌手接口。刮削不需要平台账号 | [MusicFS 的请求路径](../../plugins/scraper/online/netease.go) · [社区接口参考 api-enhanced](https://github.com/NeteaseCloudMusicApiEnhanced/api-enhanced) |
| QQ 音乐 | 通过网页搜索建议 `smartbox_new.fcg` 和 `musicu.fcg` 查歌曲、专辑、曲目表及封面 | [曲目请求](../../plugins/scraper/online/qqmusic.go) · [专辑请求](../../plugins/scraper/online/qqmusic_album.go) · [社区接口参考 QQMusicApi](https://github.com/jsososo/QQMusicApi) |
| DLsite | 按 RJ 编号查询商店页面使用的 JSON 端点 `/home/product/info/ajax`，读取作品与社团资料、封面 | [DLsite 商店端点](https://www.dlsite.com/home/product/info/ajax)（需要 `product_id`） · [MusicFS 请求与字段映射](../../plugins/scraper/online/dlsite.go) |
| 本地歌词与封面 | 从 MP3 的 USLT、APIC 帧，FLAC 注释与图片块，以及同目录的歌词、封面文件读取资料 | [ID3v2.3](https://id3.org/id3v2.3.0) · [ID3v2.4 帧定义](https://id3.org/id3v2.4.0-frames) · [FLAC 格式](https://www.rfc-editor.org/rfc/rfc9639.html) · [本地文件规则](../../plugins/scraper/local/README.md) |

## 在线音乐源

| 插件 | 当前使用的接口 | 接口文档或参考来源 |
| --- | --- | --- |
| 网易云音乐 | 通过客户端 EAPI 扫码、验证账号、查询资料并以所选音质请求可播放曲目 | [播放参数参考](https://github.com/NeteaseCloudMusicApiEnhanced/api-enhanced/blob/2db6684453297ee86dec0492842f2320b08182d9/module/song_url_v1.js) · [项目核对的版本、模块和登录范围](../providers/platform-login.md) |
| QQ 音乐 | 手机 QQ 扫码授权，通过桌面客户端 RPC 验证账号和搜索曲目 | [L-1124/QQMusicApi](https://github.com/L-1124/QQMusicApi)（社区实现） · [搜索接口讨论](https://github.com/jsososo/QQMusicApi/issues/160) · [登录协议参考版本](../providers/platform-login.md) |
| Apple Music | MusicKit JS 获取用户授权，服务端查询 `/v1/me/storefront` 校验账号和商店地区 | [MusicKit 用户认证](https://developer.apple.com/documentation/applemusicapi/user-authentication-for-musickit) · [Apple Music Storefront API](https://developer.apple.com/documentation/applemusicapi/get-a-user%27s-storefront) |
| Bilibili | 匿名调用视频详情、公开收藏夹、音频区歌单及播放信息等网页端点，按需读取可用音轨 | [MusicFS 的端点与实测范围](../providers/bilibili.md) · [yt-dlp 的 Bilibili 提取器](https://github.com/yt-dlp/yt-dlp/blob/master/yt_dlp/extractor/bilibili.py)（社区实现参考） |

网页端点可能随平台改版而变化。各插件的查询条件与可用范围见[在线刮削](../metadata/online-scrapers.md)和[在线来源](../providers/online.md)。
