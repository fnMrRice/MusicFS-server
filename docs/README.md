# 服务端文档

第一次启动服务，请从[本地运行与扫描](operations/running.md)开始。编写客户端可直接查阅 [HTTP 与 WebSocket 协议](contracts/README.md)，开发插件可从[插件架构](plugins/plugin-architecture.md)了解宿主和插件各自负责的部分。[English project overview](README.en.md)

## 部署与开发

| 主题 | 文档 |
| --- | --- |
| 运行服务 | [本地启动](operations/running.md) · [部署与 TLS](operations/deployment.md) · [配置](operations/configuration.md) · [生命周期](operations/lifecycle.md) |
| 运维 | [日志](operations/logging.md) · [备份与恢复](operations/backup.md) |
| 开发 | [Server 架构](architecture.md) · [源码导航](../src/README.md) · [开发与检查](development/README.md) · [依赖与工具](development/dependencies.md) |
| 数据库 | [SQLite 迁移](../plugins/database/sqlite/migrations/README.md) |
| 协议 | [OpenAPI 与 Schema](contracts/README.md) · [WebSocket 控制通道](contracts/control-channel.md) · [Web 接入](contracts/web-integration.md) |
| 验证 | [待验收清单](validation/README.md) · [复验方法](validation/verification.md) · [测试夹具](validation/fixtures.md) |

## 曲库、资料与播放

| 主题 | 文档 |
| --- | --- |
| 入库与查询 | [扫描](library/scanning.md) · [目录刷新任务](library/tasks.md) · [曲库查询](library/catalog-query.md) · [歌手、专辑、曲目与来源](library/music-catalog.md) · [批量选择](library/track-selection.md) |
| 曲目与专辑 | [来源曲目详情](library/source-track-details.md) · [来源专辑](library/albums.md) · [录音聚合](library/recording-groups.md) · [自动重复识别](library/duplicate-detection.md) |
| 作品库 | [内容类型与作品资料](library/works.md) · [本地作品包](library/work-packages.md) |
| 收藏与歌单 | [个人资料与播放历史](library/personal-library.md) · [个人与聚合歌单](library/playlists.md) · [来源歌单](library/source-playlists.md) · [远程同步](library/remote-playlists.md) · [推荐](library/recommendations.md) |
| 识别与匹配 | [资产摘要与指纹](metadata/identification.md) · [录音匹配](metadata/matching.md) · [候选评分](../src/metadata/identification/candidates/README.md) · [指纹算法来源](../src/metadata/identification/fingerprint/REFERENCES.md) · [测试向量](../tests/testdata/identification/README.md) |
| 歌词、封面与标签 | [资料补全](metadata/metadata.md) · [在线资料来源](metadata/online-scrapers.md) · [媒体对象](metadata/media.md) · [原件写回](metadata/writeback.md) · [补全任务](../src/metadata/enrichmentworker/README.md) |
| 音频播放 | [音频处理](playback/audio-processing.md) · [网络来源播放](playback/network-sources.md) · [本地音效](playback/local-dsp.md) · [NCM 解密](playback/ncm.md) · [格式来源](../plugins/decoder/ncm/REFERENCES.md) · [处理缓存](playback/processed-cache.md) · [播放恢复](playback/recovery.md) |

## 来源与插件

| 主题 | 文档 |
| --- | --- |
| 来源 | [在线来源](providers/online.md) · [平台账号登录](providers/platform-login.md) · [Bilibili 公开音轨](providers/bilibili.md) · [在线来源模块](../plugins/source/online/README.md) · [刮削与在线插件的 API 来源](plugins/provider-api-sources.md) |
| 开发插件 | [架构](plugins/plugin-architecture.md) · [SDK 说明](plugins/plugin-sdk.md) · [SDK 源码](../sdk/README.md) · [插件源码](../plugins/README.md) · [可选插件发行](plugins/optional-distribution.md) |
| 插件协议 | [握手与资源访问](contracts/plugins/v1/README.md) · [平台账号协议](contracts/plugins/v1/account.md) · [目录身份](contracts/plugins/v1/directory-identity.md) |
| 身份与配置 | [插件身份与名称](plugins/plugin-identity.md) · [插件管理](plugins/plugin-management.md) · [目录授权](plugins/plugin-configuration.md) · [声明式设置](plugins/settings.md) |
| 音频插件 | [编解码](plugins/audio-codecs.md) · [播放插件职责](playback/plugin-boundaries.md) · [DSP 协议](plugins/dsp-plugins.md) · [效果与声道转换](plugins/dsp-effects.md) · [DSP 配置](plugins/dsp-settings.md) |
| 其他插件 | [存储后端](plugins/storage-plugins.md) · [本地歌词与封面提取](../plugins/scraper/local/README.md) |

## 设计与图示

[当前组件图](diagrams/components.puml)展示服务端模块。[NFS / SMB 直连设计](design/nas-providers.md)、[Bilibili 来源设计](design/bilibili-provider.md)、[分阶段实施计划](design/nas-bilibili.md)及[设计图](diagrams/nas-bilibili.puml)记录扩展方案。Bilibili 已接入的能力以[实现说明](providers/bilibili.md)为准。

跨端文档由主仓库维护。[总体架构](https://github.com/fnMrRice/MusicFS/blob/main/docs/architecture.md) · [插件职责](https://github.com/fnMrRice/MusicFS/blob/main/docs/plugin-boundaries.md) · [C++ 构建约定](https://github.com/fnMrRice/MusicFS/blob/main/docs/native-plugins.md)
