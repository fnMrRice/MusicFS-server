# Bilibili 公开音频来源

`musicfs.plugin.source.bilibili` 可按编号或标准链接查找公开内容，无需 Bilibili 账号。视频播放使用上游返回的独立 AAC/M4A 音轨。

| 输入 | 结果 |
| --- | --- |
| BV、AV、视频链接（可带 `p`）、`cid` | 读取视频或指定分 P，身份为 `video:<BV>:<cid>`，上传者单独记录 |
| `au<编号>` | 读取音频区曲目，身份为 `audio:<id>` |
| `favorite:<编号>` 或公开收藏夹链接 | 按平台顺序展开全部分 P，保留重复项和失效占位 |
| `am<编号>`、`audio-menu:<编号>` 或音频歌单链接 | 读取歌单详情与分页曲目 |

本插件接受精确标识。关键词搜索、短链、私有收藏、直播及番剧暂未接入。集合按页读取，权限变化、限流或缺页时保留已发布的快照。音轨没有固定字节版本，顺序播放时才打开上游响应，当前不支持定位。

## 接口依据与验证

资料读取使用视频 `/x/web-interface/view`、收藏夹 `/x/v3/fav/resource/list` 和音频区接口。播放先读取 `/x/player/playurl` 或音频区 `url` 的格式描述，再按需连接经过检查的 HTTPS CDN。短期资源地址与请求头只在插件内使用。[来源、公开接口与参考资料](../../../../docs/plugins/provider-api-sources.md) · [宿主播放与页面操作](../../../../docs/providers/bilibili.md)

2026-10-23 的无 Cookie 抽测覆盖 BV/AV、指定分 P、`au1003142`、公开空收藏夹及含 16 项的 `audio-menu:10624`。另一次流检查读取了视频音轨开头的 16 KiB 并确认 MP4 标记。真实多页集合和浏览器整曲播放仍待验证。

在在线来源模块根目录执行：

```powershell
go test ./bilibili
```

另有两项需要真实网络的独立检查，分别读取公开资料和最多 16 KiB 的 CDN 首块：

```powershell
$env:MUSICFS_BILIBILI_LIVE = '1'
go test ./bilibili -run '^TestLiveAnonymous(Audio)?Metadata$' -count=1 -v
$env:MUSICFS_BILIBILI_STREAM_LIVE = '1'
go test ./bilibili -run '^TestLiveAnonymousAudioFirstChunk$' -count=1 -v
```
