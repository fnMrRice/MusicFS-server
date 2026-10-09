# P0 共同契约

`openapi.yaml` 使用 YAML 1.2 兼容的 JSON 语法。HTTP 模型只定义在 `http.schema.json`，不从手写 DTO 自动反向生成规范。控制请求拒绝未知字段；响应允许添加可忽略字段。OpenAPI 3.1 与插件 Schema 均使用 JSON Schema 2020-12。

本目录冻结 P0–P4 边界；文件存在不代表每个接口已经实现。尚未实现的处理、定位或插件能力应返回 `UNSUPPORTED`，实际阶段状态见实施验收记录。

## HTTP 语义

- ID 均为不透明字符串；客户端不得从 ID 解析路径，也不得自行拼接 streamUrl。输出不返回磁盘路径、引导凭据或插件能力 token。
- UTC 时间必须以 `Z` 结尾；曲目位置/时长为整数毫秒、大小为字节、码率为 bit/s。HTTP 错误 `retryAfter` 是秒；插件 `retryAfterMs` 是毫秒。
- `null` 表示未知、不适用或尚未探测；0 是已知零值。`artists: []` 表示尚无可展示歌手。`nextCursor: null` 为列表结束。
- 登录后修改状态校验允许的 Origin、HttpOnly Cookie 与 `X-CSRF-Token`。浏览器原生 audio 通过同源 Cookie 请求音频，无需自定义 Authorization。
- `Idempotency-Key` 按用户、路由和请求摘要隔离。同键相同请求重放；不同请求返回 `CONFLICT`。结果保留期限覆盖服务声明的重试窗口。未知结果先同键有界重试，不能假设客户端断连撤销了操作。
- 创建会话固定资产、表示和 attempt。seek/recover 对同一会话串行、先重放幂等结果再检查旧 attempt；晚到请求不能覆盖当前尝试。每次表示或起点改变都换 attempt 和 URL。
- `byteRange` 仅用于有固定版本保证的可 Seek 表示；`timeRestart` 返回新片段与 `positionOriginMs`。未验证能力返回 `none`。canPlayType 只作为请求线索，不能代替真实播放验收。
- 游标绑定用户、实例过滤、排序和已发布修订；游标失效时返回 `CURSOR_EXPIRED`，客户端重建列表，禁止拼接两个修订。
- 稳定文件流由 HTTP 标准库处理单范围和多范围；只有合法且无法满足的范围返回 416。If-Range 失配可回退为完整固定表示。处理中的流不继承输入 Content-Length/ETag/Range。

## 兼容与验证

同一 API/插件主版本只添加可选字段或可协商能力。增加 required 字段、更改单位/语义、删除字段或收紧已发布枚举需新主版本。废弃字段先在文档标记，保留一个兼容窗口并为旧消费者保存样本；不把未识别 capability 当成功。未知插件主版本在握手阶段拒绝。

执行 `go test ./tests/contract/api`。测试编译全部 HTTP/插件模型、验证有效/无效/新增响应字段兼容样本，并校验 OpenAPI 路径、引用和幂等/CSRF 声明。该测试的 OpenAPI 检查是项目结构校验，JSON Schema 模型验证由独立库执行；不将项目结构检查描述为完整 OpenAPI 官方元规范验证。后续 API handler 集成测试须调用 `tests/testkit.ValidateHTTP` 校验真实序列化响应，防止样本与 DTO 各自通过但相互漂移。

验证库：[`github.com/santhosh-tekuri/jsonschema/v6 v6.0.2`](https://github.com/santhosh-tekuri/jsonschema/releases/tag/v6.0.2)，MIT；只用于测试。Schema 不通过网络装载依赖。
