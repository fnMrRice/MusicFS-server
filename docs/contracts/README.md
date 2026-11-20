# 服务端 API 与插件协议

客户端通过 HTTP 访问曲库和音频流，通过 `musicfs.control.v1` WebSocket 接收状态、控制播放和管理本地租约。插件通过独立的 Worker 协议与宿主通信。

| 接口 | 机器定义 | 接入说明 |
| --- | --- | --- |
| HTTP API | [OpenAPI 3.1](openapi.yaml) · [公共模型](http.schema.json) | [Web 接口对照](web-integration.md) |
| WebSocket | [消息与事件 Schema](control.schema.json) | [控制通道](control-channel.md) · [本机租约](../operations/lifecycle.md) |
| 平台账号 | [账号状态 Schema](platform-account.schema.json) | [登录与实例作用域](../providers/platform-login.md) |
| 插件 | [协议 Schema](plugins/v1/protocol.schema.json) · [在线来源 Schema](plugins/v1/online.schema.json) · [账号 Schema](plugins/v1/account.schema.json) | [握手与运行约束](plugins/v1/README.md) · [账号扩展](plugins/v1/account.md) |

`openapi.yaml` 使用 JSON 语法书写（兼容 YAML 1.2）。Schema 使用 JSON Schema 2020-12。HTTP 响应允许新增可忽略字段，控制请求和 Worker 消息执行严格解码。

## 客户端读写约定

- **标识与单位**　ID 和服务器签发的 `streamUrl` 均按原样使用。时间为以 `Z` 结尾的 UTC 时间，位置和时长为整数毫秒，大小为字节，码率为 bit/s。HTTP `retryAfter` 以秒计，插件 `retryAfterMs` 以毫秒计。
- **未知值与分页**　`null` 表示未知或不适用，时长和大小可以为已知的零，码率为正整数或 `null`。`nextCursor: null` 表示结束。出现 `CURSOR_EXPIRED` 时按原过滤条件重建列表，避免混合两个修订的结果。
- **鉴权**　登录后的 HTTP 写操作检查 Origin、会话 Cookie 和 `X-CSRF-Token`。WebSocket 升级检查 Cookie 和 Origin，首帧提交 CSRF。音频流使用同源会话 Cookie。
- **写入与重试**　HTTP `Idempotency-Key` 和 WebSocket `idempotencyKey` 均按用户、操作和请求摘要隔离。重试沿用原键和参数，同键换参数返回 `CONFLICT`。断开连接后先核对操作结果。各端点的例外见 OpenAPI 的 `x-idempotency`。
- **并发与任务**　`If-Match` 冲突返回 412，业务修订冲突按端点定义。202 表示已接收，最终状态到操作或任务接口查询。

## 播放协商

创建播放时，在 `capabilities` 中选择 `htmlAudio: true` 或 `nativeHttp: true`，并在 `formats` 列出客户端可播的 MIME。响应中的 `outputProfile.transport` 与选定播放器一致。每次播放固定资产、输出表示与 `attemptId`。

定位与恢复串行进行，起点或表示变化时使用服务器返回的新 URL。`byteRange` 仅用于稳定且可随机读取的表示；`timeRestart` 使用新片段及 `positionOriginMs`。固定文件流支持最多 16 段 Range，非法范围返回 400，有效但不可满足的范围返回 416。`If-Range` 不匹配时可返回整个固定表示。[音频处理](../playback/audio-processing.md) · [网络来源](../playback/network-sources.md) · [恢复语义](../playback/recovery.md)

## 状态与契约测试

客户端在控制通道初次连接、重连或收到 `capabilities.changed` 后，重新读取当前用户可用的实例与能力。插件停用设置在重启时生效，管理快照通过 `configuredEnabled`、`effectiveEnabled`、`restartRequired` 区分保存值与实际运行状态。[插件管理](../plugins/plugin-management.md)

本地来源的目录授权和实际扫描范围分别管理。在线账号按 MusicFS 用户和来源实例隔离。文件摘要、来源元数据和指纹是不同证据，跨来源续播还需时间轴验证。[目录配置](../plugins/plugin-configuration.md) · [扫描](../library/scanning.md) · [资产识别](../metadata/identification.md)

在 Server 根目录执行：

```sh
go test ./tests/contract/api ./tests/contract/plugins
```

契约测试核对 Schema、真实 HTTP 响应、OpenAPI 路径、幂等和 CSRF 声明。权限、修订和任务生命周期由运行时校验。
