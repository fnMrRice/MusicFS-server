# 插件目录

插件按用途归类；目录分类与 Go module 边界分别表达。

```text
plugins/
  unified/               # 可选外部进程兼容装配：九种命令入口
  source/
    localfiles/          # 本地文件音乐源
    online/              # 独立 Go module：在线音乐源
      netease/driver/    # 网易云平台实现
      qqmusic/driver/    # QQ 音乐平台实现
      applemusic/driver/ # Apple Music 平台实现
      internal/          # 账号生命周期、分页、受限 HTTP 等共用代码
      unified/           # 统一在线入口的平台选择与装配
      cmd/               # 三个独立程序 + musicfs-plugin-online 统一程序
      tests/
  database/
    sqlite/              # SQLite 后端与独立存储程序
      migrations/        # 主库 Schema、内嵌 SQL 与迁移加载器
      state/             # 实例状态库建表、迁移与只读读取
  decoder/
    ffmpeg/              # FFmpeg 解码、编码与处理适配
    ncm/                 # NCM 容器解密
  scraper/
    local/               # 本地内嵌和旁置元数据
  dsp/
    gain/                # 增益处理节点与独立 DSP 程序
  analysis/
    chromaprint/         # 主进程内指纹分析，调用受控外部工具
  tags/
    flac/                # 主进程内 FLAC 标签重写
  matching/
    standard/            # 主进程内标准候选评分
  transform/
    thumbnail/           # 主进程内 PNG/JPEG 缩略图
```

默认运行只启动 `src/app/musicfs`：全部官方内置实现均编译到 Server 主进程，包括独立在线模块中的平台驱动。`src/app/musicfs-plugins` 仍随包构建，调用 `plugins/unified` 保留 `--plugin <用途>` 兼容入口；只有显式外部进程配置才执行。各用途独立入口也继续保留，按需显式分拆构建。公共插件 SDK 保持独立的 `sdk` 模块；源码模块、构建文件和运行进程分别管理。

内置来源、音频、Scraper 和 DSP 使用独立内存流运行原握手/数据帧协议，SQLite 直接使用后端接口；FFmpeg、ffprobe、fpcalc 仍可作为受控工具启动。实例配置、MusicFS 用户、平台账号及任务取消范围继续隔离。内置插件共享 Server 地址空间，取消依靠协作，不提供崩溃或卡死隔离；外部进程模式同样不是操作系统沙箱。

新增四类内置实现分别通过 `fingerprintworker.Extractor`、`sdk/tagformat.Format`、`candidates.Scorer`、`sdk/contenttransform.Thumbnailer` 注入。当前仅提供原有指纹、FLAC、标准评分和缩略图能力，不新增 BPM/调性、ID3/MP4 或歌词对齐。发行版本在 `sdk/bundledplugin` 各自声明，和算法/协议版本独立；内置实现升级随 Server 重新构建交付，不构成任意第三方加载机制。

SQLite 插件实现宿主的 `src/storage` 与 `src/plugins/plugindb` 契约，并使用 `src/storage/rpcbackend` 通道。具体数据库结构、主库迁移资源及实例状态库初始化由插件维护，在返回可用后端前完成；宿主保留业务数据与事务契约、后端选择、启动锁和生命周期协调。Server 仓库继续拥有这些契约及插件代码；内置后端和静态备份校验仍使用 SQLite 插件实现。公共音频接口与宿主 DSP 调度位于 `src/playback/audio`，具体增益算法位于 `plugins/dsp/gain`。

`plugins/source/online` 仅依赖 SDK，Server 受信任装配层编入所需驱动并按实例创建账号状态。可选独立程序只链接自身平台，`musicfs-plugin-online --plugin netease|qqmusic|applemusic` 则在显式进程模式运行所选平台。其余分类插件目前仍属于根 Server 模块；目录调整不代表所有角色已拆成独立 SDK 或远程仓库。构建、协议兼容、旧配置迁移与测试见 [SDK 说明](../docs/plugins/plugin-sdk.md)及各用途文档。
