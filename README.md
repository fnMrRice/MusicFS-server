# MusicFS Server

MusicFS 的服务端与插件仓库，负责来源与曲库、歌单、资料补全、音频交付，以及 OpenAPI、事件协议、插件 SDK 和数据库迁移。

- [文档导航](docs/README.md)：按运行、插件、曲库、内容、播放与设计分类查阅。
- [开始运行](docs/operations/running.md)：构建、配置、启动与扫描。
- [Server 架构](docs/architecture.md)与[总体架构](../docs/architecture.md)：内部职责及跨端约束。
- [开发与检查](docs/development/README.md)、[源码导航](src/README.md)：三个 Go 模块、测试入口与代码职责。
- [协议](docs/contracts/README.md)：HTTP、WebSocket 控制/状态和插件契约；音频仍通过 HTTP 流交付。
- [待验收清单](docs/validation/README.md)：仍需验证的能力与环境。
- [工作约定](AGENTS.md)：仓库维护规则。

默认只启动 `musicfs`，全部官方内置插件运行在 Server 主进程，用户、账号和实例边界保持独立。随包 `musicfs-plugins` 仅供显式外部进程模式使用；FFmpeg、ffprobe、fpcalc 工具仍可外部执行。源码按根 Server、sdk、plugins/source/online 三个 Go 模块维护，普通检查须覆盖三者。

本地文件与已挂载目录可扫描和播放；在线平台的登录、资料与整曲能力分别验收。原生 NFS/SMB、Bilibili 和通用第三方插件接入仍为设计。具体限制见各专题与待验收清单。

本仓库保持私有，与 Client、Web 各自管理 Git 历史。跨仓库链接按 MusicFS 根目录下 server/、client/、web/ 并列检出的布局解析。
