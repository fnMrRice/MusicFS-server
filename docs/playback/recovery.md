# 播放恢复与人工时间对齐

播放会话提供有限同源恢复和显式跨来源恢复。当前仅有本地授权音频的验证范围，网易云、QQ 和 Apple Music 尚无完整音频交付；不能据此声称跨平台续播可用。人工对齐与跨来源恢复通过 API 操作。

每次创建或续期播放会话时，Server 取 30 分钟后与当前登录到期时间中较早的时刻作为 `expiresAt`。Web 在播放时提前调用 `POST /playback/sessions/{id}/keepalive`，使用响应里的截止时间安排下一次续期。续期保留同一播放尝试与音频流，浏览器播放已缓冲的音频时也能续期。暂停后 Web 停止续期。到期、停止播放、退出登录或服务关闭时释放资源。会话数量及缓存总预算继续生效。

播放流允许浏览器缓冲填满后暂停读取，不使用 15 秒网络写入期限判断播放失败；停止、定位切换及会话到期仍会取消旧流并打断阻塞写入。客户端提交的 `positionMs` 必须为整数毫秒，Web 在 seek/recover 请求边界统一取整，避免浏览器 `currentTime` 的小数毫秒导致参数校验失败。

## 恢复预算

故障链从第一次有效恢复请求开始，整个播放会话只拥有 60 秒总预算。同源 `/playback/sessions/{id}/recover` 最多两次；跨来源最多三个不同目标，每个目标只尝试一次。目标不存在、准备失败、缓存不足等后续失败也会消耗已经取得的次数，避免用失败或新请求键无限重试。

`seek`、新 attempt、新 representation 和成功换源都不重置预算，也不延长原会话到期时间。原请求同键重放只返回已提交结果，不重新准备；会话必须仍有效且结果对应当前 attempt。已停止、过期或被后续操作替代的旧结果不会恢复成可用流。

同源恢复只复用当前私有不可变快照及未变化的来源资产。资产已换版时拒绝沿用旧偏移；原流仍使用其原生时间轴或明确重新开始。达到预算上限返回 `RECOVERY_EXHAUSTED`，需要用户重新开始播放，不进行后台无限换源。

## 人工对齐证据

同 Recording、完整文件关联、相似指纹和相同时长都不是时间轴证据。Server 不会根据这些信息生成 offset。人工证据表示用户明确作出的对齐声明，`origin` 固定为 `manualAlignment`；Server 校验其身份、区间和适用性，但不声称自动证明用户声明的音频内容正确。

证据是有方向的：

`目标位置 = 原来源位置 + offsetMs`

`range.startMs <= 原来源位置 < range.endMs`；区间和映射后的终点必须分别处于两端已知时长内，并在已验证处理工具的 24 小时定位上限内。当前只支持固定偏移，不支持速度变化、非线性映射或自动反向推导。需要反方向时创建独立证据。

证据绑定本人两端 `sourceTrackId/assetId/version`、共同的 `recordingId`、覆盖区间、偏移和完整处理配置。当前输出为含目标码率的 MP3 或 Opus/WebM profile，链修订为默认 `gain-v1` 或 `dsp-v1:<SHA256>`；配置还包含工具摘要与明确的 `gainDb`。应读取当前 recovery-profile，并将 `outputProfile` 替换为待恢复会话的实际输出 ID，不能硬编码码率或旧链身份；只允许当前编码与布局支持的合法码率。活动会话的跨源恢复保留所选码率，不按目标来源重新选择。工具、链、增益、资产或 Recording 变化后，旧证据不能用于自动继续；撤销保留历史。

## API

所有路径以 `/api/v1` 为前缀，读取需要本人登录；修改需要允许的 Origin、CSRF 与 `Idempotency-Key`。请求不接受 owner、路径、远端 URL 或音频正文。

| 方法与路径 | 作用 |
| --- | --- |
| `GET /playback/recovery-profile` | 当前可定位处理配置；默认 gainDb 为 0，提交时需显式选择与播放会话一致的增益 |
| `POST /timeline-evidence` | 提交 `recordingId/from/to/range/offsetMs/profile`，包括值为 0 的字段也必须明确提供 |
| `GET /timeline-evidence/{id}` | 本人证据及撤销状态；历史读取不保证仍适用 |
| `POST /timeline-evidence/{id}/revoke` | 正文 `expectedRevision`；初始修订 1，撤销后修订 2 |
| `POST /playback/sessions/{id}/recover-source` | 正文 `attemptId/targetSourceTrackId/positionMs/observedError` 和可选 `evidenceId` |

跨来源响应包含 `decision/reason/targetSourceTrackId/playback`：

- `continue` / `MANUAL_ALIGNMENT`：返回同一逻辑会话的新 Description，映射位置在 `positionOriginMs`。旧流上下文被取消、旧 URL 失效，客户端必须丢弃旧缓冲。
- `promptRestart`：`playback=null`。没有适用证据或可定位输出配置，不创建目标快照；当前尝试仍存在，用户可明确停止并重新点播。原因分别为 `TIMELINE_EVIDENCE_REQUIRED`、`TIMELINE_EVIDENCE_NOT_APPLICABLE`、`OUTPUT_PROFILE_UNAVAILABLE`。
- 权限、来源离线、旧资产、并发 attempt 冲突、缓存和预算问题返回原有结构化错误，不伪装成可继续结果。

读取旧证据、创建新证据或取得 Recording 关联均不授予目标来源访问权。每个恢复目标仍需通过当前资料库所有权、Provider 授权、实际文件版本检查；提交前在同一 SQLite 事务内再次检查登录、原 attempt、两端当前资产、Recording、未撤销证据和位置映射。

## 准备与取消

复制/解密目标在全局播放锁外执行，与普通创建共用一个准备槽。旧快照与新快照同时计入原有缓存总预算，不因换源暂时绕过配额。复制逐块检查取消；停止、撤销登录和故障链期限可取消准备。最终发布采用原会话/attempt 比较检查；失败释放来源租约和临时文件，删除失败的文件登记待回收并继续计入配额。

不会临时创建另一逻辑播放会话来模拟换源，也不会为未扫描文件创建假 SourceAsset。新表示发布后保留原会话有效期及累计恢复预算。
