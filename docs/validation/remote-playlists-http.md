# S-P7-04 远程歌单 HTTP 与实际服务验收

日期：2026-10-10。范围为公开接口、在线宿主适配、生产组合与持久同步链。使用合成公开歌单，不接触平台凭据、私人歌单、商业音频或用户文件；没有新增公网访问。此前网易正式公开歌单观测证据见[在线HTTP记录](online-http.md)，不能把本轮模拟远端增删当作网易实际变更。

## 实现与校验

HTTP提供显式导入、本人列表/详情、按预期绑定修订提交同步、任务查询/取消与固定revision快照分页。导入和同步有幂等键，取消自然幂等；修改操作校验Cookie、Origin、CSRF。请求不能提供路径、上游URL、配置修订或平台凭据。公开引用为RemoteSource，不建立SourceAsset；嵌套错误复制后填写当前请求编号，不修改持久错误。

`onlinehost.PlaylistRemote` 复用已经启动且限定owner/实例的在线Worker；授权不访问网络，检查匿名账号、启用状态、歌单能力和合法平台编号。执行时再次比较完整绑定与实际配置修订。单元测试明确验证其他owner、非空账号、QQ未支持能力、禁用实例、URL编号、旧配置全部拒绝，且不调用Provider；只有当前授权观测能执行，远端版本保持null。

`tests/contract/api/remote_playlists_http_test.go` 使用生产HTTP、真实SQLite和手动领取/发布票据，确定性验证：

- 导入初始revision0且不联网；同键保留身份、异参数409；省略账号、伪造字段、非匿名账号、私人实例和缺鉴权/CSRF/错误Origin拒绝。
- owner私有歌单、任务和快照统一404；列表和快照query严格，分页固定本地revision，重复曲目使用不同entryId但相同remoteSourceId。
- 同步键重放、预期绑定冲突、queued取消与终态重复取消时间保持；真实任务error与歌单error均有当前HTTP requestId。
- 成功快照发布后失败，旧分页正文不变，私有上游错误被替换为安全消息；曲库来源表仍空，没有音频资产。

真实序列化曾发现queued任务generation初值为0；公共Schema已按实际生命周期明确非负，而不是错误要求已被领取的代次。模型编译、P704有效/无效样本、全部OpenAPI引用/安全结构与新路由集合检查均通过。P706的后续模型尚未并入本步骤快照，不把其在途样本计作本轮验收。

## 真实 Server 与独立 Worker

`TestRemotePlaylistsActualServerConservativeSyncAndDisabledRestart` 从真正YAML启动正式Server、SQLite与独立 `tests/testkit/playlist-worker`。该测试程序复用生产在线协议/适配器，只在测试入口注入固定Transport；构建脚本不发布它。

四次依次独立观测为三条含重复曲目的完整结果、上游500失败、缺少一次重复出现的完整结果、时间更晚且仍缺失的完整结果。验证首次缺失仅suspectedRemoved且总数仍3，确认后新快照为2；原修订一直保留3项及原entryId。随后启动阻塞读取并取消活动任务，验证状态cancelled。最后停服并禁用在线插件，以同一数据目录重启，当前修订和历史重复项身份保持，历史仍读得到，新同步503；曲库没有隐式音频来源。

请求间保留1.1秒间隔以遵守生产Worker每秒一次的准入，而非关闭限流进行演示。该测试不是外网平台验收，不验证账号登录、授权音频或平台侧写回。

## 结果

Windows/amd64、Go 1.26.9、UCRT64 race工具链，依赖沿用当前仓库，没有修改go.mod/go.sum。

| 验证 | 结果 |
| --- | --- |
| HTTP、模型、P704样本与OpenAPI普通定向 | 4.990秒，通过 |
| 真实Server/独立合成Worker普通E2E | 9.233秒，通过，主体7.75秒 |
| 在线授权适配器race | 1.881秒，通过 |
| HTTP与契约定向race | 21.650秒，通过 |
| 真实Server/独立Worker E2E race | 14.309秒，通过 |
| Arch WSL2 Linux 采集核心/授权adapter race | 1.048 / 1.055秒，通过 |
| Linux真实SQLite远程歌单任务race | 13.616秒，通过 |
| Linux全HTTP契约 / 真实Server E2E race | 77.488 / 22.518秒，通过 |

相关生产和测试go vet、定向git diff --check通过。

队列恢复、存储并发、配置变更重验、半途分页失败和旧代次发布由独立远程歌单核心/SQLite测试覆盖，冻结步骤已通过Linux相关生产包vet与上表race。当前首版能力限已验证的网易公开unversioned观测；QQ/Apple不因存在适配程序就能导入。没有Web管理页、账号功能或音频流交付，推荐可见不等于音乐可播放。操作和限制见[远程歌单说明](../remote-playlists.md)。
