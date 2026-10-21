# 服务端契约导航

Server 维护 HTTP、事件和插件协议。精确路由及字段以本目录的机器定义为准；Schema 存在不代表所有长期能力已经实现，运行时仍以已声明能力和具体功能限制为准。

## 机器定义

| 文件 | 用途 |
| --- | --- |
| [openapi.yaml](openapi.yaml) | HTTP 路由、鉴权、幂等和请求/响应引用 |
| [http.schema.json](http.schema.json) | HTTP 公共模型 |
| [platform-account.schema.json](platform-account.schema.json) | 平台账号状态与交互模型 |
| [插件协议](plugins/v1/README.md) | 通用 Worker 控制协议及角色扩展 |
| [账号 Worker 扩展](plugins/v1/account.md) | 平台登录、恢复、退出及账号作用域 |

`openapi.yaml` 使用 YAML 1.2 兼容的 JSON 语法。OpenAPI 3.1 与插件 Schema 使用 JSON Schema 2020-12；HTTP 模型在 `http.schema.json` 定义，不从手写 DTO 反向生成。控制请求拒绝未知字段，HTTP 响应允许新增可忽略字段。

## 功能说明

| 分类 | 语义与操作说明 |
| --- | --- |
| 插件管理 | [开关、修订与重启](../plugins/plugin-management.md)、[身份与显示名称](../plugins/plugin-identity.md)、[目录与实例配置](../plugins/plugin-configuration.md) |
| 音乐库 | [查询](../library/catalog-query.md)、[扫描范围](../library/scanning.md)、[个人资料、收藏与历史](../library/personal-library.md) |
| 资产与匹配 | [原始资产和 SHA-512](../metadata/identification.md)、[片段指纹](../metadata/identification.md#本地音频指纹)、[候选与人工关联](../metadata/matching.md) |
| 内容与写回 | [媒体对象](../metadata/media.md)、[补全任务](../metadata/metadata.md)、[专辑](../library/albums.md)、[派生文件](../metadata/media.md#派生内容)、[原件写回](../metadata/writeback.md) |
| 在线来源 | [资料接口](../providers/online.md)、[平台账号](../providers/platform-login.md)、[推荐聚合](../library/recommendations.md)、[远程歌单](../library/remote-playlists.md)、[个人歌单](../library/playlists.md) |
| 音频扩展 | [处理链](../playback/audio-processing.md)、[NCM](../playback/ncm.md)、[DSP](../plugins/dsp-plugins.md) |
| 页面接入 | [Web 接口对照](web-integration.md) |

## 共同 HTTP 语义

- ID 是不透明字符串；客户端不从 ID 解析路径，也不自行拼接 `streamUrl`。普通接口不返回磁盘路径或插件能力票据；管理员目录浏览接口只公开允许选择的路径。
- 时间采用 UTC、以 `Z` 结尾；位置和时长为整数毫秒，大小为字节，码率为 bit/s。HTTP `retryAfter` 为秒，插件 `retryAfterMs` 为毫秒。
- `null` 表示未知或不适用。时长和大小的 0 是已知零值；码率只能为正整数或 `null`。`nextCursor: null` 表示分页结束。
- 登录后状态修改校验允许的 Origin、HttpOnly Cookie 与 `X-CSRF-Token`。浏览器 audio 通过同源 Cookie 读取流，凭据不放入 URL。
- `Idempotency-Key` 按用户、路由和请求摘要隔离；同键同请求重放，换请求返回 `CONFLICT`。未知结果先同键有界重试，HTTP 断开不代表操作已取消。自然幂等和无副作用 POST 的例外以 OpenAPI `x-idempotency` 为准。
- 游标绑定用户、过滤和相应修订；`CURSOR_EXPIRED` 后重建列表，不能拼接两个修订。未提供快照保证的接口在对应功能文档中说明。
- `If-Match` 失败返回 412；业务修订字段的冲突按端点定义处理。202 表示请求已接收，任务是否完成需查操作或任务结果。

## 状态与版本边界

普通插件停用只保存开关，重启后生效。`configuredEnabled` 是保存值，`effectiveEnabled` 和准入反映当前运行状态；操作 `succeeded` 不表示 Worker 已停止。读取快照的 `restartRequired` 确认当前重启需求，不用历史操作收据代替。数据库必需且不能停用；数据库与 DSP 为无普通实例的只读管理项。

Local Files 的允许目录与已选择音乐目录分开：权限数量由宿主统计，配置保存不自动扫描。目录覆盖由 `rootImportDefaultsSupported` 声明，客户端不能把全局设置冒充逐目录设置。新扫描固定有效范围；排除、隐藏或未覆盖的既有曲目不能被当作缺失删除。

网易云、QQ 音乐及 Apple Music 的业务访问要求本实例账号已校验；未登录、过期或恢复后尚未校验时拒绝访问，也不回退匿名缓存。每个实例只绑定一个用户的一个平台账号；`allUsers=true` 只扩大管理员展示范围。

`SourceAsset` 表示原始文件字节版本。Provider 申报、宿主探测、完整 SHA-512 和 DSP 前片段指纹分别记录，均不能单独证明跨源续播时间轴。NCM 解密产生独立表示，不改变原始资产身份。

创建播放固定资产、表示与 `attemptId`；seek/recover 串行处理，晚请求不能覆盖新尝试。表示或起点变化生成新 URL。`byteRange` 只用于稳定、可随机读取的表示；`timeRestart` 返回新片段及 `positionOriginMs`，不能沿用输入字节偏移。

稳定文件流支持单范围及最多 16 段范围；非法语法、反向范围或超限返回 400 `INVALID_ARGUMENT`，有效但不可满足的范围返回 416。`If-Range` 不匹配可返回完整固定表示；处理中的流不继承输入长度、ETag 或 Range。

`GET /events` 当前仅提供 `capabilities.changed` SSE，数据为 `{revision,sequence}`。首连、重连及能力状态变化通知客户端重读授权列表；它不包含管理员配置，也不代表已提供所有任务或账号事件。

## 兼容与契约检查

同一已发布 API/插件主版本只增加可选字段或可协商能力。增加必填字段、更改单位/语义、删除字段或收紧枚举需明确版本升级及配套消费者；严格 Worker 解码不能按 HTTP 的宽松响应规则处理。插件发行版本、协议版本、配置修订和资产版本分别校验，未知协议版本在握手时拒绝。

执行 `go test ./tests/contract/api ./tests/contract/plugins` 检查 Schema、样本、OpenAPI 路径、引用、幂等及 CSRF 声明。HTTP handler 测试使用 `tests/testkit.ValidateHTTP` 检查真实响应；OpenAPI 结构检查不是完整官方元规范验证。

JSON Schema 验证使用[依赖记录](../development/dependencies.md)中的 `github.com/santhosh-tekuri/jsonschema/v6`。运行时插件导入声明限 32 KiB、8 层和每对象 32 属性，采用关键字白名单，不接受外部 `$ref`、脚本或执行扩展，也不联网装载依赖。跨字段身份、范围、权限、代次及生命周期继续由运行时检查。
