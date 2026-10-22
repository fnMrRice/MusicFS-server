# 服务端文档导航

首次使用从[运行与扫描](operations/running.md)开始；理解职责看[Server 架构](architecture.md)，开发看[源码导航](../src/README.md)。当前接口以 [OpenAPI 与 Schema](contracts/README.md)为准，尚未完成的能力和环境验收集中在[待验收清单](validation/README.md)。

## 运行与部署

| 文档 | 内容 |
| --- | --- |
| [本地运行与扫描](operations/running.md) | 构建、启动、目录选择与扫描 |
| [部署](operations/deployment.md) | 双程序制品、外部工具、容器与 TLS 示例 |
| [配置](operations/configuration.md) | YAML、覆盖顺序、权限、自动发现与应用方式 |
| [日志](operations/logging.md) | 服务端与插件文件、30天压缩保留、等级与运行时管理 |
| [备份与恢复](operations/backup.md) | 停服备份、校验、新目录恢复与回滚 |
| [生命周期](operations/lifecycle.md) | 启动归属、本机租约、多客户端与退出 |

## 开发与协议

| 文档 | 内容 |
| --- | --- |
| [Server 架构](architecture.md) | 模块、身份、状态与信任边界 |
| [开发与检查](development/README.md) | 三个 Go 模块、测试布局及检查入口 |
| [依赖与工具](development/dependencies.md) | 当前依赖用途与工具版本 |
| [协议导航](contracts/README.md) | OpenAPI、HTTP 模型、事件及插件 Schema |
| [Web 接入](contracts/web-integration.md) | 接口场景索引和前端必须保留的语义 |
| [插件协议](contracts/plugins/v1/README.md) | 握手、角色、资源、扫描与配置协议 |
| [平台账号协议](contracts/plugins/v1/account.md) | 登录挑战、账号状态和凭据边界 |

## 插件

| 文档 | 内容 |
| --- | --- |
| [插件架构](plugins/plugin-architecture.md) | 当前能力、宿主责任与扩展前置条件 |
| [SDK](plugins/plugin-sdk.md) | 公共模块、协议版本及发布兼容 |
| [身份与名称](plugins/plugin-identity.md) | 插件 ID、实例 UUID、显示名与多实例声明 |
| [管理](plugins/plugin-management.md) | 状态、操作、启用、重启停用和 SSE |
| [实例配置与目录](plugins/plugin-configuration.md) | 插件配置数据库、目录授权与管理 API |
| [声明式设置页面](plugins/settings.md) | 插件字段、按钮、值校验与持久化 |
| [DSP](plugins/dsp-plugins.md) | 有序处理链、PCM Worker 与定位限制 |
| [音频编解码插件](plugins/audio-codecs.md) | 格式识别、解码链和显式转码能力 |
| [常见 DSP 与声道转换](plugins/dsp-effects.md) | EQ、压缩器、混响，独立声道转换及 stereo/5.1/7.1 输出配置 |
| [DSP 配置页面](plugins/dsp-settings.md) | 每用户链、管理员全局默认、输入输出协商与可调缓冲区 |
| [存储后端](plugins/storage-plugins.md) | 必需 SQLite、Backend 契约与独立 Worker |

## 曲库与歌单

| 文档 | 内容 |
| --- | --- |
| [扫描](library/scanning.md) | 范围、排除、监听、重试与完整发布 |
| [曲库查询](library/catalog-query.md) | 搜索、筛选、排序和固定结果分页 |
| [录音聚合曲库](library/recording-groups.md) | 人工合并后按歌曲显示与多版本展开 |
| [来源曲目详情](library/source-track-details.md) | 对应来源插件提供的分组字段、身份与版本校验 |
| [自动重复识别](library/duplicate-detection.md) | 插件候选、后台识别、进度与人工确认 |
| [来源专辑](library/albums.md) | 显式成员、发行关联和专辑封面 |
| [内容类型与作品资料](library/works.md) | 音乐／ASMR插件声明、作品建档与编辑、资料检索及实现边界 |
| [本地作品包](library/work-packages.md) | 授权目录预览与导入、多层目录、音轨关联、附件及重导入 |
| [个人数据](library/personal-library.md) | 资料、收藏与最近播放 |
| [个人与聚合歌单](library/playlists.md) | 本地编辑、出处、隐藏与重建 |
| [来源歌单列表](library/source-playlists.md) | 插件当前账号可见的歌单 |
| [远程歌单同步](library/remote-playlists.md) | 显式导入、快照、保守移除与取消 |
| [推荐](library/recommendations.md) | 按来源聚合、账号缓存及失败边界 |

## 识别与内容

| 文档 | 内容 |
| --- | --- |
| [资产识别](metadata/identification.md) | 码率、SHA-512、本地指纹和后台任务 |
| [录音匹配](metadata/matching.md) | 候选、人工关联、排除、解除与拆分 |
| [资料补全](metadata/metadata.md) | Scraper、预览、人工选用与自动补缺 |
| [媒体内容与存储](metadata/media.md) | 歌词/封面、缩略图、刮削文档、发布与回收 |
| [原件写回](metadata/writeback.md) | Windows 本地 NTFS FLAC 的授权与恢复 |

## 播放

| 文档 | 内容 |
| --- | --- |
| [音频处理](playback/audio-processing.md) | 原流、处理链、输出表示和定位 |
| [本地音效执行边界](playback/local-dsp.md) | 浏览器音效归属与服务端处理协商 |
| [NCM](playback/ncm.md) | 本地解密 Worker、独立表示及资源限制 |
| [处理缓存](playback/processed-cache.md) | 完整发布、私有复用、配额与回收 |
| [播放恢复](playback/recovery.md) | 恢复预算、独立时间证据和重播提示 |

## 在线来源与设计

| 文档 | 内容 |
| --- | --- |
| [在线来源](providers/online.md) | 当前账号资料接口和各平台能力限制 |
| [平台登录](providers/platform-login.md) | 三平台授权流程、隔离及未验范围 |
| [NAS Provider 设计](design/nas-providers.md) | 尚未实现的 NFS/SMB 直连 |
| [Bilibili 设计](design/bilibili-provider.md) | 尚未实现的视频音轨、收藏夹和音频歌单 |
| [NAS / Bilibili 实施](design/nas-bilibili.md) | 资源协议前置项、任务与候选依赖 |

设计方案不表示已实现；登录接口存在不表示真实账号已通过验收，在线整曲交付尚未实现。

## 验收、源码与图示

- [待验收清单](validation/README.md)、[复验方法](validation/verification.md)、[测试夹具](validation/fixtures.md)：只保留未关闭项目与可复用方法。
- [源码导航](../src/README.md)、[插件源码](../plugins/README.md)、[SDK 源码](../sdk/README.md)、[在线插件模块](../plugins/source/online/README.md)。
- [补全任务内部说明](../src/metadata/enrichmentworker/README.md)、[候选比较](../src/metadata/identification/candidates/README.md)、[指纹算法来源](../src/metadata/identification/fingerprint/REFERENCES.md)、[指纹夹具](../tests/testdata/identification/README.md)。
- [数据库迁移](../plugins/database/sqlite/migrations/README.md)、[本地 Scraper](../plugins/scraper/local/README.md)、[NCM 格式来源](../plugins/decoder/ncm/REFERENCES.md)。
- PlantUML 源码：[当前组件](diagrams/components.puml)、[NAS / Bilibili 设计](diagrams/nas-bilibili.puml)。
- 跨仓库入口：[总体架构](../../docs/architecture.md)、[共同插件边界](../../docs/plugin-boundaries.md)、[C++ 构建约定](../../docs/native-plugins.md)、[Web](../../web/README.md)、[Client](../../client/README.md)。
