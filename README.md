# MusicFS-server

MusicFS 的后端服务仓库，负责音乐来源、统一音乐库、歌单、信息刮削和音频流交付。主语言已确定为 Go，当前交付架构与分阶段实现文档，尚无可运行实现；具体依赖版本、平台接入和媒体兼容性在实施时验证。

设计包括：

- 插件管理：支持插件及实例的启用/禁用；默认平滑停用，停止接收新任务并等待现有播放结束，另提供强制停止。具体步骤见[插件管理计划](docs/implementation/plugin-management.md)，当前尚未实现完整管理链路。
- 配置文件采用 YAML；严格校验、迁移及管理写入规则见 [YAML 配置计划](docs/implementation/configuration.md)。当前解析器仍为 JSON，迁移完成前运行命令以现有说明为准。
- 插件化 Music Provider：本地文件、已挂载 NAS 和在线音乐来源；推荐歌单是 Provider 可选能力。
- 独立 Scraper 插件：支持自动刮削及手动搜索、预览和选用。歌词、封面等内容保存在独立文件目录，数据库保存索引与引用；默认不写入原始音频文件，可对授权本地来源配置写回。
- 插件化音频处理：解复用、解码、DSP、编码及封装，向 Web 等客户端交付兼容输出；适用时保留直接播放或代理路径。
- 记录资产码率、完整文件 SHA-512，并以音频指纹辅助匹配；跨端播放与续播行为遵守共同协议。

文档入口：

- [Server 分阶段实施](docs/implementation.md)：P0—P8 的文件职责、逐步任务、测试与完成门槛。
- [插件实施计划](docs/implementation/plugins.md)：协议宿主、Local Files、音频链、独立 Scraper 和平台适配。
- [联合实施总览](../docs/implementation.md)与[最小 Web 实施](../web/docs/implementation.md)：共同接口和真实浏览器播放验收。
- [Server 内部架构](docs/architecture.md)：模块、数据模型、Provider/Scraper、存储与任务管理。
- [Server 音频处理](docs/audio-processing.md)：处理链、播放交付、定位、缓存与资源约束。
- [内部组件图](docs/diagrams/components.puml)：可编辑的 PlantUML 源码。
- [跨端架构](../docs/architecture.md)：Server 与客户端共同遵守的协议和行为。
- [工作约定](AGENTS.md)：文档维护与仓库协作规则。

本仓库与 Client、Web 分别管理 Git 历史。跨仓库相对链接按 MusicFS 根目录下 `server`、`client`、`web` 并列检出的工作区布局解析。

工程采用根 `go.mod` 的单 module：源码放 `src/`，Server 与插件程序入口放 `src/app/<程序>/`，其余实现按职责分布到 `src/<模块>/`；测试统一放根 `tests/`，通过包接口导入被测模块。完整目录和拟用构建/覆盖率命令见 [P0 工程布局](docs/implementation/00-project-layout.md)。当前命令属于计划，不表示已创建源码或通过编译。

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
