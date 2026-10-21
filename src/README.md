# Server 源码导航

宿主源码按功能归入 7 个目录。先从 [`app/musicfs/main.go`](app/musicfs/main.go) → [`app/bootstrap/run.go`](app/bootstrap/run.go) → [`app/bootstrap/composition.go`](app/bootstrap/composition.go) 看启动和模块组装，再进入对应业务。

| 目录 | 负责什么 |
| --- | --- |
| [`app/`](app/) | 程序入口、启动组装与对外 HTTP 接口 |
| [`core/`](core/) | 身份认证、配置、共享小类型与服务生命周期 |
| [`library/`](library/) | 音乐入库、扫描、查询、专辑、歌单及个人音乐数据 |
| [`metadata/`](metadata/) | 音频识别、录音关联、歌词/封面补全及原文件写回 |
| [`playback/`](playback/) | 播放会话、音频处理、流交付、时间对齐与处理缓存 |
| [`plugins/`](plugins/) | 宿主侧的内置插件管理、可选进程通信、在线来源接入与授权读取 |
| [`storage/`](storage/) | 持久化接口、后端选择与通信、媒体文件存储与备份恢复 |

## 各模块内部

### app：服务入口

- `musicfs`、`musicfs-plugins`：Server 和可选外部插件程序入口；默认只启动 Server，全部官方内置插件在主进程运行。
- `bootstrap`：加载配置、打开存储、组装业务服务和插件、启动 HTTP 服务及处理退出。
- `api`：路由、请求校验、鉴权中间件、调用业务服务和响应转换。

### core：服务基础

- `auth`：MusicFS 登录、会话和请求安全校验。
- `config`：配置模型、读取、默认值、校验、保存及变更。
- `domain`：跨模块共用的 ID、修订号和错误等小类型。
- `lifecycle`：独立启动、客户端托管租约和退出条件。

### library：曲库与用户音乐数据

- 根包 `library`：来源、扫描入库、曲目与资产版本，以及音乐资源打开。
- `catalog`：曲库搜索、筛选、排序、分页及目录查询。
- `albums`：来源专辑、曲目归属、发行关联和专辑封面。
- `playlists`：个人/聚合歌单、远端歌单导入和同步。
- `personal`：个人资料、收藏和最近播放。
- `recommendations`：在线推荐歌单读取及缓存。
- `jobs`、`scanwatch`：扫描任务调度、目录变化监听及周期核对。
- `musicdirs`：探测常见音乐和下载目录，供初始化授权使用。
- `sourcedisplay`：将本地或在线来源引用补成可展示的曲目信息。

### metadata：识别与资料整理

- `identification`：摘要、指纹及匹配候选；宿主保留证据、预算和调度，通过 `fingerprintworker.Extractor` 与 `candidates.Scorer` 注入指纹和评分实现。
- `matching`：同录音关联、排除、解除、拆分和人工确认。
- `enrichment`：歌词/封面补全的模型、候选、策略和校验。
- `enrichmentworker`：调用刮削插件、生成预览、保存选用结果和执行自动补缺。
- `writeback`：写回授权、原件保护、输出摘要校验、替换与恢复；通过 `sdk/tagformat.Format` 注入 FLAC 标签实现。
- `writebackhost`：写回任务排队、资料准备、读写协调与恢复。

### playback：播放与音频交付

- 根包 `playback`：播放会话、来源/输出选择、输入准备、定位、停止和恢复。
- `audio`：PCM 格式与处理组件；`dspchain` 管链配置，`dspworker` 管 DSP 内置内存流和显式外部进程的通信。
- `stream`：HTTP 音频交付、Range 请求、超时和连接中断。
- `timeline`：跨来源时间对齐证据和有效区间。
- `processedcache`：完整处理输出的发布、容量、引用、淘汰和恢复。

### plugins：插件宿主与来源接入

- `pluginconfig`、`pluginid`：官方插件声明、配置表单和标识规范化。
- `plugincontrol`：启用状态、实例设置、管理操作和新工作准入。
- `pluginhost`：内置运行或显式插件进程、握手、调用、资源授权和运行状态；内置实现通过独立内存流保留原协议。
- `plugindb`：按插件实例隔离的状态接口、后端选择与远程调用；具体建表与存储实现归数据库插件。
- `pluginrpc`：持久化插件的接口校验、编码、远程调用和回调。
- `onlinehost`：在线搜索、资料、歌单及账号状态的宿主业务接入。
- `accountstore`：加密保存音乐平台凭据。
- `sourceaccess`：授权目录、文件身份/版本、越界检查及受控读取。

### storage：数据与文件持久化

- 根包 `storage`：数据库持久化接口与后端入口。
- `rpcbackend`：独立持久化插件的宿主适配。
- `mediastore`：歌词、封面、缩略图和指纹明细等文件的授权、发布、引用和清理；`content` 校验内容，通过 `sdk/contenttransform.Thumbnailer` 注入缩略图算法。
- `backup`：数据库、配置和媒体内容的备份校验与恢复。

SQLite 主库的内嵌 SQL 与版本化迁移位于 [`plugins/database/sqlite/migrations`](../plugins/database/sqlite/migrations/)，实例状态库的建表、迁移和只读读取位于 [`plugins/database/sqlite/state`](../plugins/database/sqlite/state/)。宿主通过数据库驱动调用初始化，取得数据锁后才允许写入；配置预加载保持只读。

## 目录与依赖边界

这些目录按职责聚合现有包，不表示严格的上下分层；Go 包名和既有调用关系保持，`core/config` 仍引用业务配置模型。`library`、`playback`、`storage` 的根包继续保留，各子包通过明确导入协作，不因目录嵌套自动共享实现。

[`src/plugins`](plugins/) 是宿主代码；仓库根 [`plugins/`](../plugins/) 是具体插件实现和独立入口；公共协议与可复用插件能力在独立的 [`sdk/`](../sdk/) 模块。不要把三者混放。对外 HTTP、插件协议和数据库格式的兼容性仍按对应契约维护，目录整理本身不增加功能。

官方内置实现都编入主进程，包括在线平台、SQLite、DSP 及新增的 `analysis/chromaprint`、`tags/flac`、`matching/standard`、`transform/thumbnail`。各插件独立发行版本，升级内置实现仍需升级 Server；不支持任意第三方代码加载。用户、账号、实例及任务边界保留，同进程不提供崩溃隔离，取消依赖协作。FFmpeg、ffprobe、fpcalc 等工具仍可按任务外部执行。

宿主测试继续放在根 [`tests/`](../tests/)，按 `unit`、`integration`、`contract`、`e2e` 分类，辅助代码、夹具和测试入口也留在该目录。SDK 与插件独立模块保留各自的测试边界。完整工程约定见 [开发与检查](../docs/development/README.md)，业务设计见 [Server 内部架构](../docs/architecture.md)。
