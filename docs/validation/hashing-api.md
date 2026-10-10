# S-P5-02 配置与 HTTP 专项验证

日期：2026-10-10。此记录只覆盖配置、HTTP 契约与 Windows 实际文件调用；完整计算器、存储恢复、系统读取保护及跨平台总验收由[识别证据验收](identification.md)汇总。没有运行浏览器或增加 Web 管理界面。

## 配置与契约

`go test -mod=readonly ./tests/unit/config ./tests/contract/api ./tests/contract/plugins -count=1`：配置 3.036s、API 2.333s、插件 0.647s，全部通过。

- `hashing` 省略、空对象和部分覆盖均逐项使用 `false/1/4194304`；校验并发 1—4、共享速率 65536—1073741824 的上下边界。拒绝未知子字段、null、重复键、字符串布尔、浮点/十六进制/前导零及整数溢出；正式 YAML 示例同时通过加载器与 Schema。
- 三个 HTTP 路由使用实际鉴权、SQLite 和任务服务验证。创建要求 JSON `{}` 和有效幂等键；GET 与取消返回完整任务，其他用户和不存在目标均为 404。覆盖匿名读取、无 CSRF、不允许的 Origin、未知请求字段、同键重放、在途合并、过期键 410、源版本变化后同键 409。
- 取消无需幂等键，重复取消和随后 GET 保持终态、进度、更新时间；每次响应错误使用本次 requestId。控制测试故意不启动工作线程，使排队合并和取消验证可重复；实际读取见下一节。
- JSON Schema 编译、所有契约样本、Go DTO、OpenAPI 引用和路由声明通过。当前资产的摘要冲突通过真实 HTTP 投影校验：分别引用 Provider 申报和 verified 文件证据，版本替换后当前冲突消失。

## Windows 真实文件与前台播放

`go test -mod=readonly ./tests/e2e -run '^TestYAMLHashJobsOriginalBytesCancelAndPlayback$' -count=1 -v`：测试 8.71s，包 9.623s，通过。使用临时 YAML、独立数据库/目录、真实 Local Files Worker 与 loopback HTTP；不依赖 FFmpeg，全部载荷为项目 CC0 合成音频或测试生成字节。

- 自动计算默认关闭，扫描后三个样本仍是 unknown。手动完整 WAV 与空文件计算成功，字节数与标准库独立 SHA-512 结果一致。
- 完成任务同键请求返回相同任务 ID；同尺寸改动 WAV 末字节后重扫，SourceTrack 身份保留，资产及摘要证据失效，新任务计算对应新字节摘要。
- 16 MiB 测试文件以总计 1 MiB/s 后台计算期间，原 WAV 创建播放与 Range 验证通过，观测耗时 11.8175ms。运行任务取消完成 12.8469ms；未发布完整摘要，重复取消不改变终态。这些为本机单次观测值，不是延迟服务保证。

本次专项测试直接使用当时工作区的用户依赖版本：Go 1.26.9、jsonschema/v6 6.0.3、yaml/v3 3.0.5、x/sys 0.49.0、SQLite 1.60.1；没有修改依赖文件。最终提交的冻结依赖回归应单独记录，不能把此结果改称冻结版本或 Linux/macOS/NAS 验收。
