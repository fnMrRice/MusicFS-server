# 依赖与运行工具

实际 Go 依赖版本以根 `go.mod/go.sum` 锁定，不将 README 中的在线平台参考项目当成运行依赖。没有独立子 module。

| 直接用途 | 固定版本 | 许可证与验证 |
| --- | --- | --- |
| Go 编译器、标准库 | 1.26.9 | BSD-3-Clause；Windows 与 Linux 分别验证，详见实施记录 |
| modernc.org/sqlite | 1.60.1 | BSD-3-Clause；纯 Go SQLite 驱动，WAL、迁移、事务和 VACUUM INTO 备份已测 |
| golang.org/x/sys | 0.48.0 | BSD-3-Clause；Windows 文件锁与进程树回收、Linux 文件锁 |
| github.com/santhosh-tekuri/jsonschema/v6 | 6.0.3 | Apache-2.0；只用于根 tests 的 Schema 校验，不进入业务请求解析路径 |
| FFmpeg / ffprobe | Windows 实测 6.1.1 Gyan GPLv3 构建 | 外部工具，不随 Git 仓库分发；构建参数和摘要见[插件验证](validation/plugins.md) |

传递依赖由 `go list -m all` 展示，`go.sum` 固定下载摘要。未来分发时应随所用模块版本保留对应 LICENSE/NOTICE；源码仓库许可证保持原有 GPL-3.0，不将动态音频工具许可证混同为 Go 主程序的依赖许可。

Web 是独立 Node 工程，其锁文件、开发依赖及审计结果在 Web 仓库维护。`--web-root` 只读取构建静态制品，不把 Web 源码纳入 Server 仓库。

2026-10-09 用户升级 Go 依赖；上表与当前 `go.mod/go.sum` 一致。模块下载摘要、数据库、HTTP 契约与鉴权回归通过，随后 NCM 增量的全量检查、竞争检查及四程序双平台构建也使用这组升级后的依赖，结果见[实施记录第 4 节](validation/progress.md#4-后续授权增量独立-ncm-插件)。旧验证记录中的版本是当时实测环境，不回写成新版本的历史结果。NCM 插件使用 Go 标准库独立实现，没有引入第三方解密运行依赖；协议参考见 [NCM 插件说明](ncm.md)。
