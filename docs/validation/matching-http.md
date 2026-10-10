# S-P5-04 候选与人工关联接口验证

本轮以当前共享 DTO、真实 SQLite、会话认证和实际 Server 启动验证管理接口；完整存储并发/过期证据测试及双平台/race结果由阶段总验收记录补充。使用当前仓库固定依赖，未改动依赖、Web UI 或原有指纹 E2E。

## 定向结果

- HTTP 真实数据库操作链与全部模型/样本/OpenAPI检查通过，测试包2.699秒；其中人工操作与权限链0.79秒。
- Windows 真实 Server 测试 `TestMatchingActualServerManualRoundTripRestartAndPlayback` 通过，2.67秒（测试包3.291秒）；正常操作链1.24秒，同配置重启后读取与重放0.13秒。

HTTP 测试从实际扫描发布的来源建立数据，运行真实 matching Service 和 SQLite 事务。覆盖 Cookie、CSRF、Origin；缺失/null/负数修订、未知字段、缺失或过长幂等键、重复来源；其他用户来源、录音、候选与比较池不能读取。候选刷新同快照重用 ID/创建时间且不改变人工修订，不会创建 Recording。

人工流程覆盖关联、同键重放、同键改变参数、旧修订冲突、解除、设置排除、撤销排除、再次关联和拆分。不能用排除操作暗中拆开已有录音，也不能用关联越过有效排除。响应均按 HTTP Schema 校验；关联结果保存原目标和新目标，列表与审计分别校验分页边界。最终三项来源的身份和资产保持不变，8条本人审计连续，其他用户已有操作不混入。

## 实际服务证据

E2E 使用仓库自生成的 CC0 WAV 夹具，分别写成两个独立来源文件。真实 Local Files Worker 扫描后，通过完整文件摘要 API 取得已验证 SHA-512，再经匹配刷新接口得到分数100的候选。候选仍明确 `requiresConfirmation=true`，没有自动关联。

随后通过正式管理 API 完成关联→解除→排除→撤销排除→重新关联→拆分。重启服务后，两项来源分别留在原录音和新录音，人工修订为6，7条审计持久保留。重放首次关联的旧幂等键返回原操作 ID 与当时修订1，没有撤销后续拆分或新增审计。两个源文件逐字节不变；拆分后原来源播放、真实 Range 和停止均通过。

该 E2E 不依赖 FFmpeg 或 fpcalc，验证“指纹功能未开启仍可用已验证摘要生成候选”。指纹提取与合成音频相似性测试仍归 S-P5-03；此处不把完整字节相等或人工决定当作跨来源时间轴验证。

## 复验

```powershell
go test -mod=readonly ./tests/contract/api -run 'TestMatchingHTTP|TestAllModelsCompile|TestContractSamples|TestOpenAPIReferencesAndOperations' -count=1
go test -mod=readonly ./tests/e2e -run '^TestMatchingActualServerManualRoundTripRestartAndPlayback$' -count=1 -v
```

真实 Worker/稳定文件保护与 loopback 通道需要对应本机测试权限。初次编译发生在存储候选方法仍在写入时，待实现落盘后上述两项首次执行均通过；没有跳过失败分支。本记录不宣称 Linux、其他平台、浏览器匹配页面、人工听音、外部音乐识别或 P7 歌单隐藏迁移已验收。
