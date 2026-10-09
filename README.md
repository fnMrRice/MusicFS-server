# MusicFS-server

MusicFS 的后端服务仓库，负责音乐来源、统一音乐库、歌单、信息刮削和音频流交付。当前处于架构设计阶段，尚无可运行实现；以下内容是设计范围，具体平台接入与技术选型仍需验证。

设计包括：

- 插件化 Music Provider：本地文件、已挂载 NAS 和在线音乐来源；推荐歌单是 Provider 可选能力。
- 独立 Scraper 插件：支持自动刮削及手动搜索、预览和选用。歌词、封面等内容保存在独立文件目录，数据库保存索引与引用；默认不写入原始音频文件，可对授权本地来源配置写回。
- 插件化音频处理：解复用、解码、DSP、编码及封装，向 Web 等客户端交付兼容输出；适用时保留直接播放或代理路径。
- 记录资产码率、完整文件 SHA-512，并以音频指纹辅助匹配；跨端播放与续播行为遵守共同协议。

文档入口：

- [Server 内部架构](docs/architecture.md)：模块、数据模型、Provider/Scraper、存储与任务管理。
- [Server 音频处理](docs/audio-processing.md)：处理链、播放交付、定位、缓存与资源约束。
- [内部组件图](docs/diagrams/components.puml)：可编辑的 PlantUML 源码。
- [跨端架构](../docs/architecture.md)：Server 与客户端共同遵守的协议和行为。
- [工作约定](AGENTS.md)：文档维护与仓库协作规则。

本仓库与 Client、Web 分别管理 Git 历史。跨仓库相对链接按 MusicFS 根目录下 `server`、`client`、`web` 并列检出的工作区布局解析。
