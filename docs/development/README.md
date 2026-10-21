# 开发与检查

源码职责见[源码导航](../../src/README.md)，插件扩展见[SDK](../plugins/plugin-sdk.md)，对外接口以[协议目录](../contracts/README.md)为准。

## 模块与目录

| 位置 | 边界 |
| --- | --- |
| 根 `go.mod` | Server、宿主及本地来源、解码、刮削、SQLite、DSP 实现 |
| `sdk/go.mod` | 公共协议、DTO 与 Worker 基础能力，不导入宿主业务 |
| `plugins/source/online/go.mod` | 在线平台实现，只依赖 SDK，不导入 Server 的 src |
| `src/app`、`src/core` | 程序入口、组装、API，以及鉴权、配置与生命周期 |
| `src/library`、`src/metadata` | 曲库、扫描、歌单，以及识别、补全和写回 |
| `src/playback`、`src/storage` | 播放、流、时间映射，以及持久化、迁移和备份 |
| `src/plugins` | 宿主侧插件管理、通信、账号与授权资源 |
| `plugins` | 按 source、decoder、scraper、database、dsp 分类的具体实现；unified 装配九种角色 |

三个 Go 模块通过明确的本地 `replace` 关联，构建和检查使用 `GOWORK=off`。src 的目录分组不构成严格分层，也不新增模块；例如 core/config 仍使用业务配置模型。生产包不得导入测试或程序入口包。

宿主测试放根 tests/，不在 src/ 放测试、辅助代码或夹具；SDK 和插件保持各自测试边界。测试通过真实业务接口验证行为，不为访问私有状态增加导出接口。

| 测试目录 | 用途 |
| --- | --- |
| `tests/unit` | 业务单元测试 |
| `tests/contract` | HTTP、Schema 与插件协议一致性 |
| `tests/integration` | 数据库、文件系统与真实进程组合 |
| `tests/e2e` | 跨制品服务流程；浏览器测试由 Web 仓库维护 |
| `tests/testkit`、`tests/testapps`、`tests/testdata` | 辅助代码、测试程序和共享夹具 |

## 构建与检查

在 Server 根目录运行：

```powershell
./scripts/build.ps1
./scripts/check.ps1
```

```sh
sh scripts/build.sh
sh scripts/check.sh
```

默认构建 musicfs、musicfs-plugins 两个程序。`-SplitPlugins` / `--split-plugins` 额外生成六个分拆程序；正式制品见[部署](../operations/deployment.md)。检查脚本覆盖三个模块，根目录的 `go test ./...` 不会跨越子模块。

真实音频测试按需设置 MUSICFS_FFMPEG、MUSICFS_FFPROBE，指纹测试另设 MUSICFS_FPCALC。缺少工具时跳过的测试不计为通过；公网或真实账号测试先核对[验证方法](../validation/verification.md)中的入口限制。

覆盖率按改动范围运行：

```text
go test -coverpkg=./src/...,./plugins/... -coverprofile=build/coverage.out ./tests/...
```

竞争检查需要支持的平台、本机 C 编译器与 CGO。PowerShell 构建脚本会设置 CGO_ENABLED=0，执行竞争检查前切换为 1，并使用覆盖三个模块的检查入口：

```powershell
$env:CGO_ENABLED = '1'
./scripts/check.ps1 -Race
```

集成测试的开关、实际工具、跳过项和目标平台必须明确；交叉编译不代替目标机运行。共享素材及生成方法见[夹具](../validation/fixtures.md)，未完成环境验收见[待验收清单](../validation/README.md)。

## 契约与维护

- 文档移动后运行 `python scripts/check-docs.py`，检查分类导航、相对链接及跨仓库入站链接；原分类与合并工具保留在 `scripts/reorganize-docs.py`，只有显式传入 `--apply` 才修改文件。
- Server 维护 OpenAPI、事件 Schema、插件协议和数据库迁移；变更须同步检查 SDK、客户端兼容性及总体架构。
- 协议版本与实现版本分别维护；未知能力返回明确错误，不以空结果冒充支持。
- 发布时重新校验资产、任务代次、修订和权限；数据库事务不跨平台网络或普通 Provider 调用持有，存储后端的受控回调按 Backend 契约协调。
- 新的 C++ 插件使用 CMake/C++23 独立 Worker，遵循[共同构建约定](../../../docs/native-plugins.md)，与 Client 原生库分别构建和验收。
- 文件拆分、移动和提交遵循[工作约定](../../AGENTS.md)。
