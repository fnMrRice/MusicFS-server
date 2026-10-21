# 依赖与运行工具

根 Server、SDK、在线插件三个模块分别由 go.mod/go.sum 固定依赖。下表说明当前用途，完整版本以各模块文件为准；平台 API 和 NCM 的参考项目不是运行依赖。

| 依赖 | 当前版本 | 用途 |
| --- | --- | --- |
| Go | 工具链 1.26.9，语言版本 1.26.0 | 编译器与标准库 |
| modernc.org/sqlite | 1.60.1 | SQLite 驱动 |
| github.com/google/uuid | 1.6.0 | 插件实例及业务 UUID |
| golang.org/x/sys | 0.49.0 | 文件锁、稳定读取和进程管理 |
| golang.org/x/text | 0.43.0 | SDK 文本处理 |
| go.yaml.in/yaml/v3 | 3.0.5 | YAML 语法树、校验和保留注释写入 |
| gopkg.in/natefinch/lumberjack.v2 | 2.2.1 | 日志文件轮转；事件记录使用标准库 log/slog，压缩与保留由 src/core/logging 统一管理 |
| github.com/santhosh-tekuri/jsonschema/v6 | 6.0.3 | 协议测试及受限配置 Schema 校验 |
| FFmpeg / ffprobe | 使用过 Windows 6.1.1、Linux 6.1.6 构建 | 外部探测、解码和编码，能力按实际构建检测 |
| Chromaprint fpcalc | 1.6.1，CLI 算法 2 | 默认关闭的本地指纹提取，显式固定 SHA-256 |

传递依赖可在各模块运行 go list -m all 查阅。发布流程收集实际编译依赖的许可证并写入 release.json，测试依赖、参考项目和外部工具不混为运行时 Go 依赖，见[部署](../operations/deployment.md)。

FFmpeg、ffprobe 和 fpcalc 不随仓库分发；其构建能力与许可单独核对。Chromaprint 来源、参数及许可见[算法来源](../../src/metadata/identification/fingerprint/REFERENCES.md)，NCM 格式来源见[插件参考](../../plugins/decoder/ncm/REFERENCES.md)。

Web 的 Node 工具链与锁文件由 Web 仓库维护，Server 只读取其构建静态制品。
