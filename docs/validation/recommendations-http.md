# S-P7-03 推荐接口与组合验收

日期：2026-10-10。范围是按来源聚合推荐、缓存、HTTP与实际Server/独立Worker组合。当前账号为空匿名，没有个性化推荐或平台登录；本轮不再次请求公网，不把合成响应称为平台在线实测。此前正式网易公开推荐、QQ失败与Apple授权缺失的外网证据见[在线HTTP记录](online-http.md)，使用说明见[推荐接口](../recommendations.md)。

## 交付与测试

新增 `GET /api/v1/recommendations`，严格解析可重复instanceId、单个limit/refresh；来源顺序保留，单项失败仍返回其他来源。来源错误仅含稳定code、安全message和秒单位retryAfter，没有上游原始正文。缓存键包含owner/实例/账号/配置修订/limit，生产限制64项、8 MiB、2活动请求，读取超时与等待均有限。关闭时先取消推荐请求并等待，再关闭在线Worker。

`tests/contract/api/recommendations_http_test.go` 使用实际HTTP路由、鉴权和SQLite，合成Provider精确统计调用次数。验证首次读取及缓存命中、显式选择顺序、失败保留旧items/lastSuccessAt并标stale、refresh不能越过冷却（即使Provider已恢复）、未知/重复/过量参数拒绝、匿名401、其他owner实例404与错误脱敏。结束后来源表仍为空，读取没有隐式导入。

`tests/e2e/recommendations_test.go` 使用真实YAML、正式Server组合、独立合成网易Worker及正式QQ/Apple Worker；合成Worker只替换上游Transport，宿主和协议链保持生产实现。一次读取同时返回网易成功、禁用实例SOURCE_OFFLINE、QQ推荐UNSUPPORTED和AppleAUTH_REQUIRED。随后验证缓存避免触发Worker速率限制、即时刷新收到限流仍保留旧items、冷却到期后恢复；停服并禁用全部在线插件，重新启动没有旧缓存或旧成功时间。本项不访问外网、不用凭据、不下载音频。

`recommendations_schema_test.go` 核验OpenAPI聚合路由及可重复query表达；新增JSON样本覆盖匿名/非个性化/无游标边界与UTC时间。真实HTTP序列化响应均通过独立JSON Schema验证，另运行全模型编译、所有契约样本及既有OpenAPI引用/安全结构检查。

## 联调发现与修复

合法配置中的显示名可能为空白、过长或含控制字符。推荐服务原来会连带拒绝启动，现以实例编号回退，实例身份与配置修订仍严格验证；实际YAML服务用含两端空白的名称验证此边界。

首次真实HTTP与E2E发现lastSuccessAt/expiresAt输出本地 `+08:00`，违反全站UTC `Z` 契约。核心发布现统一UTC；新增非UTC上游到期时间回归，再运行相同实际序列化验收通过。失败记录没有被当作通过，也没有放宽时间Schema。

统一Linux并发检查随后暴露HTTP测试原100ms夹具冷却短于重负载下Schema验证时间。该测试现使用1分钟冷却，并在Provider恢复后继续检查不能越过已提示的冷却，不增加sleep；过期后真正恢复仍由核心及真实Server测试覆盖。此修正仅改变测试时序，没有放宽生产限流。

## 本轮结果与限制

Windows/amd64、Go 1.26.9、UCRT64 race工具链，沿用仓库固定依赖，无go.mod/go.sum修改。

| 检查 | 结果 |
| --- | --- |
| 推荐HTTP普通定向 | 1.456秒，通过 |
| 实际Server/Worker普通E2E | 9.289秒，通过，主体8.15秒 |
| 推荐核心完整unit race（含名称与UTC新增回归） | 1.391秒，通过 |
| 推荐HTTP、全模型/样本/OpenAPI结构 race | 32.117秒，通过 |
| 实际Server/Worker推荐E2E race | 19.017秒，通过 |
| Arch WSL2 Linux 推荐核心 / 全HTTP契约 race | 1.406 / 66.342秒，通过，冻结步骤快照 |
| Arch WSL2 Linux 实际Worker / Server组合 race | 14.222 / 38.020秒，通过，离线合成上游 |

核心缓存并发、取消、容量和隔离测试另由推荐模块测试覆盖；Linux冻结快照还通过相关包go vet与上表组合验证。匿名能力不能代替实际账号隔离/凭据过期验收；这些账号功能目前未实现。没有新增Web页面、远程导入或播放链改动，没有将推荐内容解释为可交付音频。静态检查最初被并行生命周期步骤尚未接齐的启动参数阻断；桥接完成后，推荐、API、bootstrap及对应测试的go vet通过。本步骤冻结快照保留生命周期改动前的组合文件，不混入该后续功能。定向git diff --check通过。
