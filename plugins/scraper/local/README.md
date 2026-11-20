# 本地歌词与封面

`musicfs.scraper.local` 从宿主授权的音频和同目录文件提取歌词与封面。扫描时可自动补齐缺项，手动搜索则先返回候选，由用户预览、选用。

| 输入 | 读取内容 |
| --- | --- |
| MP3 | ID3v2.3/v2.4 的 `USLT` 歌词和 `APIC` 图片。支持未压缩、未加密的帧 |
| FLAC | Vorbis `LYRICS`、`UNSYNCEDLYRICS` 注释和 `PICTURE` 块 |
| 同目录旁置文件 | 音频同名的 `.lrc`、`.txt`、`.jpg`、`.jpeg`、`.png`，以及 `cover.jpg/png`、`folder.jpg/png` |
| 容器封面 | 上游解码器提供的独立附件，例如 NCM 外层封面。候选标记为 `origin=container`、`basis=containerEmbedded` |

旁置歌词接受 UTF-8 或带 BOM 的 UTF-16。ID3 歌词也按帧声明接受 Latin-1。歌词中的时间标记按原文保留。图片须通过 JPEG/PNG 完整解码检查。

## 接口与限制

宿主通过 `Open` 建立最长 30 秒的任务会话，`Search` 返回候选描述，`Fetch` 读取同一会话的候选正文。插件使用任务绑定的资源引用，不接收任意文件路径。二进制通道限定为本机回环，禁用代理和重定向。宿主二次校验正文，并在发布引用前保护来源文件和容器附件。

| 项目 | 上限 |
| --- | --- |
| 单份歌词 | 1 MiB |
| 单张图片 | 8 MiB、1600 万像素、单边 8192 像素 |
| 单次会话 | 32 个资源（最多一个音频）、64 个候选、32 MiB 候选正文 |
| 音频标签扫描 | 16 MiB |
| 并发会话 | 默认 2，可配置为 1–4 |

## 格式依据

[ID3v2.3](https://id3.org/id3v2.3.0) · [ID3v2.4 帧定义](https://id3.org/id3v2.4.0-frames) · [ID3v2.4 结构](https://id3.org/id3v2.4.0-structure) · [FLAC（RFC 9639）](https://www.rfc-editor.org/rfc/rfc9639.html)。在线刮削插件使用的资料接口另见[来源与公开 API](../../../docs/plugins/provider-api-sources.md)。宿主的选用、人工锁定与自动补缺见[资料补全](../../../docs/metadata/metadata.md)。
