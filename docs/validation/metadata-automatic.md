# S-P6-04 导入歌词与自动补缺验收

日期：2026-10-10。配置、公开结果和使用语义见[补全说明](../metadata.md#导入歌词与自动补缺)；本页只记录本轮自动流程，不覆盖 [P6-03 手动任务](metadata-http.md)的历史证据。

## Windows 定向验证

本机 Go 与依赖使用当前已提交版本；没有新增 npm/Node 运行依赖。真实测试以 YAML 启动 Server，构建并启动独立 Local Files 和 Local Scraper Worker；样本来自仓库合成 WAV/MP3，加合成 ID3 USLT/APIC、同名 LRC 和 PNG，不访问在线服务或私人音频。

| 范围 | 测试及结果 |
| --- | --- |
| 配置默认、严格字段和结构、启用依赖、显式导入与全局开关分离、Current/Snapshot 切片隔离 | `go test -mod=readonly ./tests/unit/config -run 'TestAutomaticMetadata\|TestInstanceImportDefaults\|TestOfficialYAML' -count=1`：通过，2.353s |
| 真实 SQLite + HTTP 所有者隔离、连续小页、跨扫描游标、鉴权、未知/重复/畸形查询；全部模型/样例/OpenAPI | `go test -mod=readonly ./tests/contract/api -run 'TestMetadataAutomation\|TestAllModelsCompile\|TestContractSamples\|TestOpenAPI' -count=1`：通过，4.741s |
| 新声明仍保留递归归一、授权根与浅扫描的保守对账 | `TestYAMLPluginImportOptionsHTTPAndScopedLibrary`：通过，4.81s |
| 真实自动补全完整流程 | `TestAutomaticMetadataActualServerImportDepthLocksAndRestart`：通过，14.92s（包15.660s） |

真实服务验证覆盖：

- 全局关闭且单次 importLyrics=false：只产生 skipped 自动触发，内容选择为空。
- 全局仍关闭，但单次 importLyrics=true：独立导入同名 LRC 和内嵌 USLT；不继承全局 `languages:[en]` 的过滤，不误导入封面，不自动加人工锁。
- recursive=false 只含根层；maxDepth=1 仅扩至一层，未扫描深层不顺带导入。
- 人工选用后变更候选 LRC，再导入明确 manualLocked；旧媒体对象、修订和锁保留。只有该测试主动改写的 LRC 作为新输入，其余源始终不变。
- 重启后开启全局策略，单次 importLyrics=false 仍补深层缺失歌词和内嵌 APIC；已有歌词不替换，通用 cover.png 不自动认定为每首歌曲的封面。
- 再停用 Scraper 并重启，历史结果/内容/锁仍在，新显式导入逐项 SOURCE_OFFLINE；WAV 原流 Range 和停止继续通过。
- 最终逐文件 SHA-256 对照预期，自动流程没有修改音频或旁置文件；没有使用音频听感代替字节证据，也未声称人工听音。

首次真实自动选用暴露一个事务嵌套问题：来源保护回调在持有 SQLite 事务时再次查询唯一连接，使随后的认证查询超时并显示401。修复后将来源保护放在选择事务外，同时保留已成功操作的幂等重放路径；上表14.92s是修复后的完整重跑，不是把首次失败记为成功。没有通过重新登录或放宽超时规避问题。

## 范围与后续统一检查

最终独立冻结快照已通过 Windows 格式、vet、完整测试与完整 race：端到端124.050秒/竞争153.987秒，自动补全服务69.565秒/竞争82.677秒，SQLite竞争183.547秒。文件数增长使旧 PowerShell 检查展开的绝对路径超过 Windows 命令行长度限制，入口改为让 gofmt 遍历 src/tests 两个目录，并检查退出码；修正后整套通过。

Linux WSL2 Arch 的同一快照通过 vet 和十个关键包 race：配置2.270秒、插件单元1.163秒、策略单元1.050秒、解析1.061秒、来源读取15.817秒、补全服务121.145秒、真实插件230.495秒、SQLite102.602秒、HTTP契约43.346秒、端到端344.129秒。Windows使用现有FFmpeg/ffprobe6.1.1；Linux使用项目目录内编译的6.1.6，均配置真实工具和Chromaprint1.6.1，没有通过跳过真实音频代替验证。

当前不据此宣称 Web 自动补全页面、远端 Scraper、原文件写回、动态插件启停或所有 NAS 平台可用。

现有未上线开发库不提供旧 schemaRevision 别名或任务计划兼容转换。当前请求使用插件公开的 local-import-v2；错误的旧修订明确冲突，测试不会填旧修订来伪装兼容。
