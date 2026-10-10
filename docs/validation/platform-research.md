# S-P7-01 平台调查验证记录

日期：2026-10-10；范围：只读公开资料、固定版本源码和三次无账号元数据请求。能力结论见[平台能力调查](../platform-capabilities.md)，后续任务见 [P7 实现计划](../implementation/07-online-library.md)。本记录不表示在线 Provider、账号登录、歌单同步或平台播放已经实现。

## 1. 证据来源

官方项目 GitHub API 于 2026-10-10 01:21:28–01:21:32 UTC 核查仓库状态、默认分支、commit、tag 和许可；固定版本详见能力调查。选定源码仅下载到 Git 忽略的 `build/_online-research/`，没有安装或运行三个参考项目，没有新增运行时依赖。QQ 和 Apple 参考仓库的 tags 响应为空，不能把 package.json 的版本号写成已发布 tag。

Apple 官方文档于 01:26:05–01:26:08 UTC 保存认证、令牌、分页和请求/响应说明。网页需要 JavaScript，Markdown 版本的 Songs/Playlists 属性页只返回标题，不能据空页核实字段；本轮 03:47:35 UTC 再读取官方 DocC JSON，确认属性定义。该补充发现：当前 Playlists.Attributes 有 lastModifiedDate，但未列 versionHash；调查正文据此不承诺远程强快照。

| 补充官方证据 | SHA-256 |
| --- | --- |
| [Songs.Attributes 文档数据](https://developer.apple.com/tutorials/data/documentation/applemusicapi/songs/attributes-data.dictionary.json) | `d09af49bc2051c5f64bc136d12f41f1f373c80113413dbb16d68847da8245406` |
| [Playlists.Attributes 文档数据](https://developer.apple.com/tutorials/data/documentation/applemusicapi/playlists/attributes-data.dictionary.json) | `bf7939923b110bb8258103d0ffcdd14fcb370fa272b54737539f811aee3f20e4` |

忽略目录内保存 `*-reference.json`、仓库树与指定源码、`apple-official/sources.json`、`apple-official/attribute-sources.json` 和 `anonymous-probes.json`。这些是本机复核材料，不作为部署文件或提交附件。公开仓库源码中的示例令牌、示例歌曲地址也未复制到正式文档或产品配置。

## 2. 匿名探测

复用同日本任务前段已执行的下列三次请求，整理文档时没有重复探测。请求仅发往平台自有主机，不带 Authorization 或 Cookie，关闭 Cookie 容器和自动重定向；搜索词为 `MusicFS`，只请求第一页、最多一条。未访问用户私人曲库、推荐历史或账户设置，未调用音频 resolver，未下载商业音频、封面或歌词，未上传音频指纹。

| UTC 时间 | 公开 GET 请求 | HTTP / 业务结果 | 能证明的范围 |
| --- | --- | --- | --- |
| 01:24:48.1626232 | `music.163.com/api/search/get?s=MusicFS&type=1&limit=1&offset=0` | 200 / `code=200`；1224 bytes JSON；响应声明 songCount=59 | 此旧单曲搜索入口当时可匿名返回可解析数据；不是新版加密模块或全部搜索类型的验证 |
| 01:24:48.5099594 | `c.y.qq.com/soso/fcgi-bin/client_search_cp?format=json&n=1&p=1&w=MusicFS&cr=1&g_tk=5381&t=0` | 200 / `code=0`；1579 bytes，可解析 JSON；声明总数 146 | 此单曲搜索入口当时可匿名返回；不证明其他 QQ 接口仍可用 |
| 01:24:52.7732326 | `api.music.apple.com/v1/catalog/us/search?term=MusicFS&types=songs&limit=1` | 401；空正文，没有 JSON | 无开发者令牌被拒绝，符合官方认证说明；未验证带令牌搜索成功 |

两个声明总数都不是实际完整读取数量；没有翻页，也没有比较多次结果。一次成功不证明稳定性、固定配额、其他地区可用、当前订阅权限或音频播放。

响应只保留长度、摘要、根字段名、业务码和数量，不保存原始搜索条目、响应 Cookie 或可复用媒体地址：

| 请求 | 响应正文 SHA-256 |
| --- | --- |
| 网易云匿名搜索 | `4fe1b5f9943b542a2a52008aa1fd9562f625bd8dde450330ec7d9bf668dea6b2` |
| QQ 匿名搜索 | `372e38b50cbe956c77661052a7ed4f9dcb422f28be9e85287c72caa8bcc1d148` |
| Apple 无令牌目录查询 | `e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855`（空正文） |

本轮还阅读了固定提交的网易云搜索/详情/歌单/推荐/刷新模块，QQ 对应 routes，以及 Apple 参考播放器调用 MusicKit 的代码。源码说明和实际在线探测分列，未运行登录、刷新、vkey、unblock 或平台写入接口。

## 3. 后续 fixture 方法（尚未生成生产测试集）

| 待验证行为 | 可提交的脱敏 / 合成样例 | 仍需真实授权验收 |
| --- | --- | --- |
| 资料映射 | 白名单字段；稳定假平台 ID；大整数以字符串保存；null/0/空串分开；假标题/歌手/地区/质量与试听标记 | 同一合法账号的实际字段、地区和权限含义 |
| 分页与保守同步 | 两页间增删、重复页、循环 next、顺序变化、重复曲目、半途失败、无快照版本、过期任务晚返回 | 真实多页响应和平台提供的版本保证（若有） |
| 授权与缓存隔离 | 假 owner/instance/account；401/403/限流/过期；缓存不得跨账号复用 | 两个授权测试账号，退出/撤权/刷新后权限确实变化 |
| 推荐 | 公共与个性化分别建假列表；无下一页、部分失败、stale、推荐消失不删已导入 | 实际匿名推荐和个人推荐，分别记录来源与账号范围 |
| 音频交付边界 | `example.invalid` 假 URL、假过期时间和私密头；本地合成音频服务测试 Range、到期、重定向、编码变化 | 原平台明确授权的完整资源/试听/订阅/地区/到期；Apple 当前裸流仍未获证据 |

录制响应先在本机按白名单投影，替换账号、私人歌单、昵称、文本和 ID；删除 Cookie、Authorization、签名、设备标识、真实媒体 URL 与私人内容。必须保留结构、类型、缺失值和失败码，不能为了通过测试把限流或试听改成成功完整流。每个样例标注 `synthetic` 或 `captured-sanitized`、平台、参考提交、日期与匿名/授权类别，不记录真实账号身份。

fixture 验证只证明适配器对样例的处理，不替代在线账号、分页完整性和播放验收。音频测试使用项目合成音频或明确可分发样本；本阶段不上传指纹、不自动抓取私人内容、不用商业录音充当仓库夹具。

## 4. 完成范围与未验收项

- 已完成：三平台能力矩阵；固定版本、许可和日期；官方文档/参考源码/匿名实测分层；Go 独立适配优先次序；真实授权输入与脱敏 fixture 方案。
- 匿名实测仅覆盖网易云和 QQ 各一个搜索入口，以及 Apple 无令牌的拒绝响应；资料详情、公开推荐、歌单和分页完整性仍未在线验证。
- 账号登录、二维码/短信交互、刷新、双账号隔离、个性化推荐、地区/订阅限制、限流恢复均未实测。
- 没有平台音频解码、Range、seek、试听与完整流判别证据；Apple 目录、previews 和 MusicKit 播放不能替代 Server 裸流证据。
- 没有在线 Provider 生产变更、Web 新页面、动态插件启停或外部写入；后续须逐平台、逐能力实现和验收后才声明 supported。
