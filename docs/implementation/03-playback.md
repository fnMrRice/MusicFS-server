# P3：原始音频流与最小 Web 闭环

状态：固定尝试、Cookie 流授权与原流 Range 已落地；浏览器/权限/重启验证见[实施记录](../validation/progress.md)。返回[实施入口](../implementation.md)。只声明实测格式，不将扩展名当作兼容性证明。

## 1. 拟建模块

| 位置 | 职责 | 根 tests |
| --- | --- | --- |
| src/playback | Resolve、会话、尝试、用户偏好、停止与幂等 | unit/playback、integration/playback |
| src/stream | 固定表示的 GET/HEAD、Range、鉴权与取消 | unit/stream、integration/playback |
| src/api | 会话创建/停止/恢复、流地址返回 | contract/api、integration/api |
| src/sourceaccess | 受限固定版本输入和打开资源生命周期 | integration/scan、integration/playback |
| src/storage/sqlite | 会话元信息、幂等结果、过期状态 | integration/storage |

## 2. 实现步骤

### S-P3-01：选择来源和交付路径

1. POST /playback/sessions 接收 sourceTrackId、终端能力和偏好，先校验用户可见性、来源当前状态与资产可用性。
2. 未匹配到 Recording 的 SourceTrack 也能播放；无 SHA-512、指纹、歌词或封面不能成为拒绝播放的前置条件。
3. 根据容器/codec/MIME/传输能力判断原流可用性。浏览器实际支持以 P0/P3 实测 profile 为准；扩展名不替代探测。
4. 本地/NAS 必须由 Server 交付。P3 不返回磁盘 file URL，不把仅 Server 可访问的 NAS 路径传给浏览器。
5. 不支持的原格式返回需要处理/暂不支持的明确状态；P4 接入后沿相同接口选择 processed，无需 Web 直接调用 FFmpeg。

测试：未知元数据、无 Recording、权限不足、文件不可用、支持/不支持格式和不同用户请求。验收：只有授权且可执行的播放计划才生成流描述。

### S-P3-02：会话和固定尝试

1. 建立 PlaybackSession（用户/设备、目标、有效期、恢复预算）与 PlaybackAttempt（资产、表示、profile、起点、代次）的分离模型。
2. 创建时固定 assetId、representationId、deliveryMode=proxy、outputProfile、seekMode 和 streamUrl。后续库刷新不悄悄替换活动尝试。
3. Idempotency-Key 绑定用户、请求摘要和有效期；重试同一创建不生成多套无主资源，改参数则报冲突。
4. 会话元信息可持久化；操作系统句柄、HTTP 请求和子进程不能作为可序列化状态。重启后旧活动流标失效并明确要求重建。
5. DELETE 会话幂等且需所有权校验；重复删除本人已结束会话可返回成功，但不能暴露别人的会话存在性。
6. 建立过期回收和受限活跃会话数，不依赖浏览器一定发送停止请求。

测试：并发创建、重复键、参数冲突、会话过期、重启、跨用户停止和停止/GET 竞争。验收：同一播放代次不会被晚到旧请求覆盖。

### S-P3-03：流授权和输入生命周期

1. streamUrl 为限定资源的同源地址；每个 GET/HEAD 校验会话 Cookie、用户、session、attempt、状态和有效期，不能仅凭难猜的 ID 授权。
2. 打开数据通过 SourceAccess，保存当前尝试读取的资源身份。重命名/替换路径不能使旧尝试读到另一个文件。
3. 发现资产或输入版本变化时拒绝新 Range/结束受影响流，通知恢复流程；不能把新内容续接旧缓冲。
4. 文件大小和 mtime 只作为变更线索。需要跨请求保持强一致时，必须有 Provider 的稳定版本保证或已验证不可变表示；能力不足可限制 seek，必要时构建受控缓存副本。
5. 已校验完整内容摘要或不可变表示版本可用于强验证器；不把 mtime + size、临时 URL、随机 ID 假装为内容恒定保证。
6. 同一文件对象的并发请求采用独立读取偏移或受控 ReaderAt，不能让两个 HTTP Range 共享可变 Seek 指针而串数据。
7. 请求断开立即传播取消并释放该请求引用；同会话其他活跃请求不被误关。会话停止/撤销则关闭全部关联资源。

验收：同用户正常流可读，跨用户/会话失效即拒绝；替换文件、并发拖动及取消测试不产生混合字节。不能证明版本稳定性的存储明确列限制，不声称弱观测能覆盖所有原地改写。

### S-P3-04：HTTP Range、HEAD 和条件请求

1. 对确实固定、可 Seek、长度已知的表示使用 net/http.ServeContent 或等价实现，进入前完成授权/版本校验；不要把边生成流伪装成 ReadSeeker。
2. GET 返回正确 Content-Type、可声明的 Content-Length、Accept-Ranges 与条件验证器；HEAD 复用鉴权和元信息，不读取完整音频。
3. 验收完整 GET、区间/open-ended/suffix 单 Range、206 Content-Range、超出边界的 416 与 `bytes */size`。
4. 无 Range 能力时不伪造 206。多区间或非法 Range 的支持策略明确记录；只实现单区间时返回一致的明确错误，不错误地把合法不可满足与解析失败混为一谈。
5. 有可信验证器时支持 If-Range；失配按 HTTP 语义返回完整新表示或先因已失效 attempt 拒绝，不能给新文件旧偏移的 206。行为写入契约测试。
6. 流开始后失败不能追加 JSON；中止流并记录可供 recover 判断的错误。控制接口另返回结构化结果。
7. 保持有界拷贝缓冲、背压和 context 取消；不能先把整曲读进内存再回应。

测试：所有范围边界、零长度文件、未知长度、并发请求、慢消费者、早断连、试图复用其他 attempt 的 URL。验收：下载片段与夹具指定字节一致，浏览器拖动确实触发正确读取。

### S-P3-05：最小恢复行为

1. recover 接收客户端能观察的错误和曲目时间，不要求浏览器辨认上游具体状态码。
2. P3 实现过期会话/已停止/资产变化/不支持/暂不可用的明确响应。尚无候选来源时返回 unavailable 或 promptRestart，不无限尝试。
3. 如允许同源授权刷新，先确认源字节与输出表示未变；否则创建新尝试并重新加载。每次恢复的次数和总预算归属原会话故障链。
4. seek/recover 共用每会话的串行状态变更，校验预期 attemptId；同操作重试先按用户/路由/请求摘要重放幂等结果，再判断是否需新建尝试，避免成功后重试被误判为旧 attempt。相同键改参数报冲突。幂等结果保留期限覆盖声明的重试窗口，无法恢复结果时明确要求重建。
5. 跨来源自动续播推迟 P7 的有效时间映射实现，本阶段不得仅因 Recording 相同自动恢复。
6. Web 每会话只发一个在途控制操作；同曲响应确认新 attemptId 后再执行最新待定位目标。取消 HTTP 不承诺撤销服务端变更；结果不明先同键有界重试，仍不明则停止自动控制。旧歌曲/已停止会话的晚结果不能改变新会话。

测试：响应丢失后同键重试、控制请求重复/冲突、recover 与 seek 竞争、提交后浏览器断开、重试窗口过期、恢复中切歌；同次恢复不重复消耗预算或创建多套输出。

### S-P3-06：与最小 Web 联调

1. 使用[Web 计划](../../../web/docs/implementation.md)的真实登录、列表和原生 audio，不建立另一套 demo API。
2. 通过 dev proxy 或同源静态部署播放；验证 audio 自身请求携带 Cookie，不依赖 fetch 自定义头或整曲 Blob。
3. 操作序列覆盖登录→刷新列表→播放→暂停/恢复→跳到中段→连续切歌→停止→注销。
4. 试听时确认正确曲目、声道、标记位置与音量；同时检查浏览器事件/请求、Server 状态、进程/句柄和内存。
5. 重启 Server 后旧 attempt 失败可解释，新建会话可再播放；音乐库和数据仍保留。

## 3. P3 门槛与记录

- [x] 实际Edge浏览器解码本地音频得到非零PCM；输入、浏览器与Server版本有记录。
- [x] 非零定位、暂停/恢复、快速切歌、停止和会话失效通过。
- [x] 列表、会话、流按用户及具体登录会话授权，不暴露原路径。
- [x] Range、并发读取、变更、慢消费者和取消测试通过。
- [x] 未覆盖格式/浏览器/平台单列；后续跨源恢复只使用独立适用的人工时间证据。
- [ ] 人工扬声器听音；非零PCM、频率标记和浏览器事件不替代人工确认。

浏览器证据见[Web兼容记录](../../../web/docs/compatibility.md)与[NCM验证](../../../web/docs/ncm-validation.md)，Server真实原流见[实施记录](../validation/progress.md)；P7有限恢复增量见[恢复说明与实测](../recovery.md)。初始 `src/stream` 职责实际由 `src/playback` 和API流处理实现，不为目录名创建空包。

依据：[ServeContent](https://pkg.go.dev/net/http#ServeContent)、[HTTP Range](https://www.rfc-editor.org/rfc/rfc9110.html#name-range)。真实浏览器验证不能由 handler 单元测试替代。
