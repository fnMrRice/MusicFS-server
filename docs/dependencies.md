# 依赖与运行工具

实际 Go 依赖版本以根 `go.mod/go.sum` 锁定，不将 README 中的在线平台参考项目当成运行依赖。没有独立子 module。

| 直接用途 | 固定版本 | 许可证与验证 |
| --- | --- | --- |
| Go 编译器、标准库 | 1.26.9 | BSD-3-Clause；Windows 与 Linux 分别验证，详见实施记录 |
| modernc.org/sqlite | 1.60.1 | BSD-3-Clause；纯 Go SQLite 驱动，WAL、迁移、事务和 VACUUM INTO 备份已测 |
| golang.org/x/sys | 0.48.0 | BSD-3-Clause；Windows 文件锁与进程树回收、Linux 文件锁 |
| go.yaml.in/yaml/v3 | [3.0.4](https://pkg.go.dev/go.yaml.in/yaml/v3@v3.0.4) | [MIT 与 Apache-2.0](https://github.com/yaml/go-yaml/blob/v3.0.4/LICENSE)；YAML组织维护，配置语法树解析、严格校验及保留注释的开关写入 |
| github.com/santhosh-tekuri/jsonschema/v6 | 6.0.3 | Apache-2.0；只用于根 tests 的 Schema 校验，不进入业务请求解析路径 |
| FFmpeg / ffprobe | Windows 实测 6.1.1 Gyan GPLv3 构建 | 外部工具，不随 Git 仓库分发；构建参数和摘要见[插件验证](validation/plugins.md) |

传递依赖由 `go list -m all` 展示，`go.sum` 固定下载摘要。未来分发时应随所用模块版本保留对应 LICENSE/NOTICE；源码仓库许可证保持原有 GPL-3.0，不将动态音频工具许可证混同为 Go 主程序的依赖许可。

Web 是独立 Node 工程，其锁文件、开发依赖及审计结果在 Web 仓库维护。`--web-root` 只读取构建静态制品，不把 Web 源码纳入 Server 仓库。

YAML依赖为本轮配置层新增，固定版本与许可已按官方模块页面核对；项目自己的大小、类型、字段和安全限制不等同于依赖库支持的全部YAML语法。验收见[配置记录](validation/configuration.md)。

2026-10-09 用户升级此前的SQLite、系统调用与Schema校验依赖，模块下载摘要、数据库、HTTP契约与鉴权回归通过；随后NCM增量的全量检查、竞争检查及四程序双平台构建使用这批升级后的依赖，结果见[实施记录第4节](validation/progress.md#4-后续授权增量独立-ncm-插件)。YAML模块是之后配置增量新增，不追记为旧NCM验证使用的依赖。上表对应当前`go.mod/go.sum`，旧验证记录仍保留当时实测环境。NCM插件使用Go标准库独立实现，没有引入第三方解密运行依赖；协议参考见[NCM插件说明](ncm.md)。
