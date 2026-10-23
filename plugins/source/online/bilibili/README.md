# Bilibili 来源插件

独立实现 `musicfs.plugin.source.bilibili`。默认匿名访问公开内容，不读取本机浏览器账户、Cookie、用户配置，也不引入外部提取器运行依赖。

## 已实现范围

- 精确曲目查询：BV、AV、规范 HTTPS 视频链接、`p` 分 P、`cid` 身份，以及 `au` 音频区曲目。关键词搜索、短链和番剧入口不在本轮范围内。
- 视频 AV 规范化为 BV，每个分 P 使用 `video:<BV>:<cid>`；音频区使用 `audio:<id>`。视频上传者仅保存在详情的上传者字段，不推断为演唱者，也不把视频标题作为发行专辑。
- 精确集合查询：`favorite:<id>`、公开收藏夹规范链接、`am` 或 `audio-menu:<id>`。搜索只读取集合资料；读取曲目和观察集合时才完整分页。
- 收藏夹按上游顺序展开全部分 P，保留重复出现及失效条目占位。选定展开策略、远端写入、私有收藏夹登录授权尚未实现。
- 音频歌单按上游条目完整分页，保留重复。未知总数使用空值；缺页、权限变化、风控、限流及网络失败不会被发布为一个空集合。
- 观察结果声明 `unversioned`。游标绑定插件实例内的短期快照；翻页上限、条目上限和总耗时都受约束，超限观察标记不完整。
- `DescribeAudio` 只读取平台 JSON 供播放格式协商；播放器实际请求音频时才调用 `OpenAudio`。从平台 JSON 选取公开独立 AAC/M4A 音轨并开启单次 CDN 响应，直接交给宿主按需读取。插件不预读、预下载、散列或验证音频内容，不宣称固定字节版本，也不跨响应 Range 续接。

## 网络边界

JSON 请求固定访问 `api.bilibili.com` 和 `www.bilibili.com`，不跟随重定向。音频响应仅允许 HTTPS Bilibili CDN 域名，逐跳检查重定向；DNS 解析结果必须全部是公共地址，连接绑定已检查的地址，不使用系统代理。测试传输器仅由宿主构造时注入，不能通过客户端 RPC 配置。

平台登录失败使用 `PLATFORM_AUTH_REQUIRED`，与 MusicFS 会话分离；权限不足、风控、限流、资源失效和临时网络故障分别返回明确错误。平台 JSON 或 HTTP 状态禁止访问时不尝试绕过。短期音频地址和请求头不进入公开曲目 DTO。

## 验证记录

2026-10-23 在无 Bilibili Cookie 的条件下，通过插件实际请求并执行 SDK 结果校验：

| 入口 | 结果 |
| --- | --- |
| `BV13x41117TL` / `av8903802` | 规范为 `video:BV13x41117TL:14694589` |
| `BV1bK411W797?p=2` | 规范为 `video:BV1bK411W797:191559297` |
| `au1003142` | 曲目资料读取成功 |
| `favorite:1103407912` | 上游当前返回公开空收藏夹 |
| `audio-menu:10624` | 完整读取 16 项 |
| 视频与 `au1003142` 播放描述 | JSON 返回独立 AAC/M4A 描述；没有请求或读取 CDN 音频 |
| `video:BV13x41117TL:14694589` CDN 首块 | 单独开启真实流检查，成功读取 16,384 字节、确认 MP4 初始化标记后立即关闭；未下载完整音轨 |

真实多页收藏夹、真实多页音频歌单、私有内容及浏览器端实际播放仍需各自验收。两页集合、分 P、重复、失效占位（含 `type=0`）、部分失败、私有属性、请求隔离、受控重定向以及音频按需读取由离线测试覆盖，不能用离线通过代替真实平台可用性保证。CDN 首块成功只证明当次受控音频响应可读，不代表浏览器、完整播放或定位已验收。

相关端点由实际公开请求验证：`/x/web-interface/view`、`/x/v3/fav/resource/list`、`/x/player/playurl`，以及音频区 `song/info`、`menu/info`、`song/of-menu`、`url`。平台可能调整这些接口；仅在已验证的独立音轨返回时提供播放，不自动退回混合视频流。

在在线插件模块目录运行离线测试：

```powershell
go test ./bilibili
```

显式开启匿名在线资料验证（仅 JSON 请求，不读取音频内容）：

```powershell
$env:MUSICFS_BILIBILI_LIVE = '1'
go test ./bilibili -run TestLiveAnonymous -count=1 -v
```

独立开启真实 CDN 首块检查（上限 16 KiB，读完立即关闭）：

```powershell
$env:MUSICFS_BILIBILI_STREAM_LIVE = '1'
go test ./bilibili -run TestLiveAnonymousAudioFirstChunk -count=1 -v
```
