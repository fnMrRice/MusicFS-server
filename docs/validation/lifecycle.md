# S-P8-06 生命周期验收

范围是 Server 的实际启动归属、鉴权租约和有界退出。没有实现桌面 Client 的发现、窗口、安装或升级行为。

## 自动验证

独立测试位于：

- `tests/unit/lifecycle`：多客户端、续租序号重试、初次无租约、崩溃过期、独立运行、会话注销与并发续租围栏、实例不符、容量边界、状态返回防御性复制。
- `tests/contract/api/lifecycle*_test.go`：Cookie/Origin/CSRF、真实 loopback 地址检查、同用户跨会话拒绝、missing/null/未知字段拒绝、draining 释放幂等、实际模型对正式 HTTP Schema、四条 OpenAPI 路由与非法请求。
- `tests/e2e/lifecycle*_test.go`：正式 YAML 加载与 SQLite、真实 loopback HTTP、两客户端释放和注销、崩溃一方不结束另一租约、无人认领启动超时、standalone 不受租约数量控制、重启不继承旧归属、实际 CLI 进程退出和 PID 记录。
- `tests/unit/bootstrap/lifecycle_test.go`：CLI 默认归属及非法模式，不公开不安全的租约时间参数。

真实 HTTP 超时测试使用可信测试时钟推进默认 30 秒，仍走正式就绪、鉴权、定时检查和清理路径；不把等待 30 秒后的 mock 状态断言作为端到端测试。CLI 用正常默认时间，释放最后租约后观察实际进程退出。

清理故障测试在可信测试钩子中暂停收尾，80 毫秒后确认返回明确超时；随后同 dataRoot 启动第二 Server 必须失败。释放钩子后等原清理真实完成，再确认该目录能够重新启动。此测试证明锁保留，不证明被暂停的写入已经完成。

2026-10-10 Windows amd64 最终定向 race 通过：核心 1.738 秒、bootstrap 6.512 秒、HTTP 契约 9.525 秒、真实服务及 CLI 23.953 秒。包括生产单调时钟保留和真实 CLI 子进程正常退出；`go vet` 的 lifecycle/api/bootstrap 检查通过。Arch WSL2 Linux联合冻结快照亦通过：核心race1.044秒、bootstrap1.065秒、HTTP契约1.556秒，实际CLI退出16.73秒、多客户端1.78秒、无首租约/单方崩溃3.28秒、卡住清理仍持锁1.69秒；对应E2E与安装/备份合跑70.657秒。已区分Linux原生执行与交叉编译。

```powershell
go test -mod=readonly -race ./tests/unit/lifecycle ./tests/unit/bootstrap ./tests/contract/api ./tests/e2e -run TestLifecycle -count=1
go vet -mod=readonly ./src/lifecycle ./src/api ./src/bootstrap
```

测试仅使用合成/空库和独立临时目录，无真实音乐、账号或公网请求。Windows race 需要已配置的 C 编译器；受限沙箱无法完整运行本机服务时，在获得执行许可的宿主环境运行。
