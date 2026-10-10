# MusicFS-server

MusicFS 的 Go 后端服务。当前已落地 Local Files 扫描、SQLite 音乐库、会话鉴权、原流 Range、独立 Go DSP 处理链和本地 NCM 解密插件，配合独立 Web 仓库提供最简列表播放器。启动步骤见[本地运行说明](docs/running.md)，每步提交与验证边界见[实施记录](docs/validation/progress.md)。

配置已统一为 `configVersion: 1` 的 YAML，插件与实例显式登记和启用；严格解析、修订写入与崩溃恢复见[配置说明](docs/configuration.md)和[配置验收](docs/validation/configuration.md)。旧 JSON 配置不兼容。读写服务只保存期望状态，当前仍需重启应用，尚未提供动态启停管理 API/命令。

长期设计包括（未列入实施记录的能力仍为计划）：

- 插件管理：支持插件及实例的启用/禁用；默认平滑停用，停止接收新任务并等待现有播放结束，另提供强制停止。具体步骤见[插件管理计划](docs/implementation/plugin-management.md)，当前尚未实现完整管理链路。
- 插件化 Music Provider：本地文件、已挂载 NAS 和在线音乐来源；推荐歌单是 Provider 可选能力。
- 独立 Scraper 插件：支持自动刮削及手动搜索、预览和选用。歌词、封面等内容保存在独立文件目录，数据库保存索引与引用；默认不写入原始音频文件，可对授权本地来源配置写回。
- 插件化音频处理：解复用、解码、DSP、编码及封装，向 Web 等客户端交付兼容输出；适用时保留直接播放或代理路径。
- 记录资产码率、完整文件 SHA-512，并以音频指纹辅助匹配；跨端播放与续播行为遵守共同协议。

文档入口：

- [Server 分阶段实施](docs/implementation.md)：P0—P8 的文件职责、逐步任务、测试与完成门槛。
- [YAML 配置](docs/configuration.md)：字段、插件与实例、覆盖顺序、配置读写及应用边界。
- [插件实施计划](docs/implementation/plugins.md)：协议宿主、Local Files、音频链、独立 Scraper 和平台适配。
- [联合实施总览](../docs/implementation.md)与[最小 Web 实施](../web/docs/implementation.md)：共同接口和真实浏览器播放验收。
- [Server 内部架构](docs/architecture.md)：模块、数据模型、Provider/Scraper、存储与任务管理。
- [Server 音频处理](docs/audio-processing.md)：处理链、播放交付、定位、缓存与资源约束。
- [内部组件图](docs/diagrams/components.puml)：可编辑的 PlantUML 源码。
- [跨端架构](../docs/architecture.md)：Server 与客户端共同遵守的协议和行为。
- [工作约定](AGENTS.md)：文档维护与仓库协作规则。

本仓库与 Client、Web 分别管理 Git 历史。跨仓库相对链接按 MusicFS 根目录下 `server`、`client`、`web` 并列检出的工作区布局解析。

工程采用根 `go.mod` 的单 module：源码放 `src/`，Server 与插件程序入口放 `src/app/<程序>/`，测试统一放根 `tests/`。使用 `scripts/build.ps1` / `scripts/build.sh` 构建四个独立程序：`musicfs`、`musicfs-plugin-local`、`musicfs-plugin-audio-ffmpeg`、`musicfs-plugin-ncm`；使用 `scripts/check.ps1` / `scripts/check.sh` 检查。真实音频工具测试需设置 `MUSICFS_FFMPEG` 和 `MUSICFS_FFPROBE`，否则明确跳过该部分。

第一条交付链为 Local Files 扫描 → 音乐库列表 → 授权原流 → Web 有声播放；随后补齐解码 → PCM DSP → 编码/封装 → Web 播放和时间定位。P5—P7 的匹配、刮削及在线来源逐阶段扩展，不阻塞首个本地可播放版本。

## 开源项目参考

在线音乐平台插件可参考以下开源项目的接口与实现。核查日期：2026-10-09；当前仅列为实现参考，尚未引入这些项目的代码或运行时依赖，也未完成平台账号与播放能力联调。

| 平台 | 开源项目及许可证 | 计划参考内容 | 当前边界 |
| --- | --- | --- | --- |
| 网易云音乐 | [NeteaseCloudMusicApiEnhanced/api-enhanced](https://github.com/NeteaseCloudMusicApiEnhanced/api-enhanced)，[MIT](https://github.com/NeteaseCloudMusicApiEnhanced/api-enhanced/blob/main/LICENSE) | 搜索、歌曲元数据、歌单分页、推荐和账号上下文的请求 / 响应结构 | 按实际授权和接口验证结果开放能力；不因参考项目存在某接口便承诺 MusicFS 支持 |
| QQ 音乐 | [jsososo/QQMusicApi](https://github.com/jsososo/QQMusicApi)，[GPL-3.0](https://github.com/jsososo/QQMusicApi/blob/master/LICENSE) | 接口组织、搜索 / 歌单解析、分页和账号 Cookie 作用范围 | 先作接口参考；复制、修改或作为运行依赖前核对目标版本许可和交付要求 |
| Apple Music | [zachomedia/apple-music-webplayer](https://github.com/zachomedia/apple-music-webplayer)，[MIT](https://github.com/zachomedia/apple-music-webplayer/blob/master/LICENSE) | MusicKit JS 授权、音乐目录 / 资料库访问及浏览器播放器交互 | 这是 Web 播放器示例，不是 Go 音频流 SDK；其代码不能证明 Server 可获得或转码完整音乐裸流 |

上述仓库页面在核查时可访问且未显示归档标记；持续维护状态和具体接口的现网可用性需在接入时重新验证。每个平台实际采用参考内容时，补充准确 commit / tag、所参考文件、采用方式、验证结果及必要的署名 / 许可证记录。仅列出仓库名称不能代替依赖和代码来源记录。

Apple Music 的资料接口与播放分别核对 [Apple Music API](https://developer.apple.com/documentation/applemusicapi) 和 [MusicKit 官方文档](https://developer.apple.com/musickit/)；完整音乐播放按官方播放器和用户授权能力单独适配，不将 catalog / preview 地址等同于可经 Server DSP 的整曲资源。插件分阶段步骤见[插件实现计划](docs/implementation/plugins.md)。

### 本地 NCM 解密参考

NCM 插件已按以下 MIT 项目的容器布局和解密流程，用 Go 标准库重写为独立 Worker，并接入扫描、播放与 Web。它负责本地加密容器，不属于网易云在线 API 插件；没有把参考项目作为运行依赖。配置、手工验证及限制见 [NCM 插件说明](docs/ncm.md)。

| 参考项目 | 许可证 | 参考内容与采用方式 |
| --- | --- | --- |
| [taurusxin/ncmdump-go](https://git.taurusxin.com/taurusxin/ncmdump-go) | [MIT](https://git.taurusxin.com/taurusxin/ncmdump-go/src/branch/main/LICENSE) | 参考 `ncmcrypt/ncmcrypt.go` 的文件头、密钥/元数据区和封面填充布局；重写解析和随机读取，加入严格边界检查，不采用联网补封面或改写音乐标签的逻辑 |
| [iqiziqi/ncmdump.rs](https://github.com/iqiziqi/ncmdump.rs) | [MIT 声明](https://github.com/iqiziqi/ncmdump.rs/blob/master/Cargo.toml) | 对照 `crates/ncmdump/src/ncmdump.rs` 的音频载荷偏移与读取/定位语义；不引入 Rust 运行依赖 |

核心、真实 Worker、Server 端到端和 Edge 三项 NCM 联调已通过；浏览器样本由独立 Node 生成器封装本项目 CC0 MP3/FLAC，另经 Go 核心逐字节校验。已执行 299,534 次模糊测试，Linux 真实 Worker 测试通过。升级依赖后的全量检查、竞争检查和四程序 Windows/Linux 构建均通过，详细证据见[实施记录](docs/validation/progress.md)。后续真实双根扫描发布 1,943 条来源，并通过 6 个真实文件（含 4 个 NCM）的 HTTP 播放、定位及原件不变校验，见[真实库验收](docs/validation/real-local-library.md)；结论不外推到所有网易云客户端下载版本。
