# P7-03：匿名推荐服务验收

2026-10-10；本记录覆盖 `src/library/recommendations` 的服务与真实 Worker 组合。HTTP、配置组装和显式歌单导入由后续接合步骤验收；本步骤不声称这些入口已经完成。

## 公开能力与边界

- `New` 接收由宿主绑定的 Runtime；`List` 只读公开推荐，`Shutdown(ctx)` / `Close()` 取消共享获取并等待结束。服务不持有资料库、播放或歌单写入接口。
- 当前 `AccountID` 必须为空，表示匿名作用域；非空返回 `UNSUPPORTED`。网易云为已验证的公开第一页，`personalized=false`、`nextCursor=null`。QQ / Apple 的能力限制延续 [在线适配验收](online-core.md)，不会为了返回推荐而跨平台替换。
- `Source.instanceId`、`Source.accountId` 和项目的 `playlist.externalId` 可构造后续显式导入的远程引用。读取、刷新或推荐消失均不导入、删除个人库或尝试播放。
- 缓存键包含 owner、instance、account、配置修订和请求 limit。启动配置固定，服务重建不会沿用旧配置缓存；没有动态插件管理或账号切换实现。
- 返回平台原顺序与推荐说明；不产生自定义分数或虚假分页。独立校验来源 URL、顺序、时间、数量及匿名语义。

## 资源与失败语义

默认最多 64 项缓存、8 MiB 序列化页面、2 个并行获取；配置硬限为 256 项、64 MiB、4 个获取。缓存通过 LRU 回收，活动获取不被驱逐；达到上限返回明确限流。每次最多选 8 个实例、每实例 1–50 个条目。缓存 TTL 默认且最多 2 分钟，并受 Provider 页面到期时间限制。

同一作用域的并发请求共享一次获取。一个调用方取消只结束自己的等待；所有等待者离开时取消该次获取，新请求不会继承已取消结果。关闭服务关闭新准入、取消所有获取并等待；Worker 进程由宿主组合层另行关闭。获取超时默认 10 秒、上限 30 秒；包含并发槽等待的总预算最多为该值的两倍。

来源失败保留上次成功条目、`lastSuccessAt` 和有效期，设置 `stale=true` 与脱敏错误；首次失败没有伪造的成功时间。不同来源各自返回结果，保持请求或配置顺序。显式刷新也遵守默认 2 秒失败冷却和上游 `Retry-After`（最多 1 小时），恢复后清除旧错误。服务重启清空该内存缓存。

## 实测

测试全用合成元信息，无外网、私人音乐、凭据或账号。真实进程集成运行 `tests/testkit/online-worker`，使用固定合成 HTTP 响应；它验证宿主/Worker 通道与限流传播，不替代 P7-02 的真实平台探测。

| 验证 | Windows amd64 | Linux amd64 / WSL Arch |
| --- | --- | --- |
| 推荐单元测试，race | 通过，7.148 秒 | 通过，1.388 秒 |
| 真实 Worker 推荐缓存/限流/关闭，race | 通过，11.201 秒 | 通过，18.136 秒 |

命令使用 Go 1.26.9 与 `-mod=readonly`；Windows race 使用既有 MSYS2 GCC，Linux 使用既有原生工具链。测试文件：

- `tests/unit/recommendations/cache_test.go`：跨用户/实例/请求限制、返回值深复制、TTL、失败保旧、刷新冷却、恢复、部分失败、LRU 数量与字节预算。
- `tests/unit/recommendations/concurrency_test.go`：共享获取、单等待者取消、全部取消后的新请求、并发上限、关闭回收、超时与缓存繁忙。
- `tests/unit/recommendations/validation_test.go`：恶意来源页、错误顺序、伪分页/个性化、过期/未来观察、匿名限制和配置/请求边界。
- `tests/integration/plugins/recommendations_test.go`：真实 Worker 成功后缓存命中，主动刷新收到 Worker 限流仍保留旧结果，关闭 Worker 后冷却不被绕过，服务关闭拒绝新请求。

本步骤无需新增 Go 依赖，也不包含自动重试、个性化推荐、登录凭据缓存、音频权限结论或部署性能承诺。
