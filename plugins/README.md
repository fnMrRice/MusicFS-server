# 插件目录

插件按用途归类；目录分类与 Go module 边界分别表达。

```text
plugins/
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
  decoder/
    ffmpeg/              # FFmpeg 解码、编码与处理适配
    ncm/                 # NCM 容器解密
  scraper/
    local/               # 本地内嵌和旁置元数据
  dsp/
    gain/                # 增益处理节点与独立 DSP 程序
```

除在线模块外，各插件的独立程序入口放在对应目录的 `cmd/musicfs-plugin-xxx`。Server 和本地插件组合程序入口仍在 `src/app/musicfs`、`src/app/musicfs-plugins`，只负责装配。公共插件 SDK 保持独立的 `sdk` 模块。

SQLite 插件使用宿主的 `src/storage` 接口、`src/storage/rpcbackend` 通道与 `src/storage/migrations` 迁移定义；数据库结构和迁移的所有权仍属于 Server。公共音频接口与宿主 DSP 调度位于 `src/audio`，具体增益算法位于 `plugins/dsp/gain`。

`plugins/source/online` 仅依赖 SDK，各平台独立程序只链接自身驱动。统一程序 `musicfs-plugin-online --plugin netease|qqmusic|applemusic` 包含三个平台的装配代码，每个进程只运行所选平台。其余分类插件目前仍属于根 Server 模块；目录调整不代表所有角色已拆成独立 SDK 或远程仓库。构建、协议兼容、旧配置迁移与测试见 [SDK 说明](../docs/plugin-sdk.md)及各用途文档。
