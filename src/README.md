# 服务端源码导航

从 [main.go](app/musicfs/main.go) 进入程序，[bootstrap/run.go](app/bootstrap/run.go) 加载配置，[bootstrap/composition.go](app/bootstrap/composition.go) 组装存储、插件与业务服务。

| 目录 | 从这里查找 |
| --- | --- |
| [app](app/) | 程序入口、依赖装配、HTTP 路由和鉴权中间件 |
| [core](core/) | 配置、会话认证、共享标识与服务生命周期 |
| [library](library/) | 扫描、曲目与资产、专辑、作品包、收藏及远程歌单同步 |
| [metadata](metadata/) | [指纹与匹配候选](metadata/identification/candidates/README.md)、[歌词和封面补全](metadata/enrichmentworker/README.md)、标签写回与恢复 |
| [playback](playback/) | 播放会话、解码及 DSP 链、HTTP 音频流、时间轴和处理缓存 |
| [plugins](plugins/) | 插件注册与管理、进程通信、来源账号及文件访问授权 |
| [storage](storage/) | 持久化接口、远程后端适配、媒体对象、备份与恢复 |
| [tasks](tasks/) | 任务队列的模型、查询与取消 |

## 常用入口

- **作品资料与导入**　[作品 API](app/api/works.go) 调用[作品服务](library/works/)；[作品包 API](app/api/work_packages.go) 交由[包服务](library/workpackages/)预览、确认和关联附件。[流程说明](../docs/library/work-packages.md)
- **插件实现**　宿主在 [src/plugins](plugins/) 校验并调度实例，具体算法和平台驱动在仓库根的 [plugins](../plugins/README.md)，公共协议在 [sdk](../sdk/README.md)。
- **SQLite**　主库结构与升级在[迁移目录](../plugins/database/sqlite/migrations/README.md)，插件实例状态库在 [state](../plugins/database/sqlite/state/)。
- **测试**　宿主的单元、契约、集成与端到端用例在 [tests](../tests/)。构建三个 Go 模块和运行检查的命令见[开发指南](../docs/development/README.md)。
