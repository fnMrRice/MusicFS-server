# 插件控制协议 1.0

官方 Worker 与 Host 使用 JSON-RPC 2.0。每帧为 `Content-Length: <UTF-8 正文字节数>\r\n\r\n` 后跟 JSON 正文；单帧最多 1 MiB，每页最多 200 项。stdout 只承载帧，英文诊断写 stderr；大块音频、PCM、图片与文件正文不能进入 JSON/Base64。

Host 发出的 ID 为 `h-` 前缀，Worker 调用 Host 时用 `w-`。一帧只承载一个请求、响应或取消通知，不支持 batch。干净 EOF 表示对端结束；标头/正文半帧 EOF 是协议失败。必须设置握手及请求 deadline，读取取消/超时由关闭受控通道传播，不无限等待部分帧。

`host.hello` 请求绑定 instanceId、配置修订和本次随机 nonce。成功结果为 Ready（逻辑上的 worker.ready），必须回显 instanceId、nonce，并用 manifest 声明精确 protocolVersion。1.0 只协商 1.0；主版本不匹配必须拒绝。每个角色实例独立 Worker，实例/账号权限由 Host 启动上下文固定，不能被请求字段切换。账户引用不是凭据。

`provider.scan` 返回带 scanId/generation/page 的观测页，末页 complete 还须结合 issues 和根身份决定是否可以发布完整扫描。持久身份由 Host 命名空间状态维护。`provider.resolve` 返回受限资源引用，rootId/relativePath 只在受信任控制面使用，浏览器看不到它们；Host 实际打开时仍需验证根、版本和授权。

`$/cancelRequest` 是无 ID 通知，其 params.id 指向在途调用。已返回后取消幂等；已取消的晚结果只可回收，不可发布。重复终态、重复活跃 ID、未知响应 ID 都视为协议错误。取消不是强制 OS 沙箱；拒绝退出的受管 Worker 由 Host 在宽限期后回收。

JSON-RPC 数值码：-32700 解析错误、-32600 非法请求、-32601 未知方法、-32602 非法参数、-32603 内部协议错误；业务失败统一 -32000，并在 data 给出稳定 code/retryable/retryAfterMs。明确不支持的已识别能力返回 `UNSUPPORTED`。不能以空成功响应代替未实现功能。

Host 的 `host.entries` 返回固定根身份的 Entry 页；`host.state.get/compareAndSet` 使用实例隔离的 key/revision/value。value 是限额 512 KiB 的状态字节（Go `[]byte` 的 Base64 JSON 编码），仅保存身份等小型状态，不能承载音频正文。`host.resource` 由 Host 检查受限相对路径后分配不透明引用；Worker 不自行签发引用。

音频 Worker 的 hello 额外接收已由 Host 探测的 capabilities。`audio.describe` 返回实际工具摘要、profile 和 PCM；`audio.prepare/start` 接收固定计划与 loopback input/output 任务通道。音频二进制由任务限定的 loopback HTTP 传输，不写入控制面。宿主注入的 FFmpeg 绝对路径和可执行文件摘要仅在受信任配置边界使用，不发送给 Web。取消仍使用统一取消通知；具体字段是插件内的 `gainDB`，HTTP 用户偏好是 `gainDb`，两者在边界显式转换。

本版本已经定义首个 Provider、Host 状态/资源以及音频处理的具体 wire 模型；刮削协议在后续阶段增加。Schema 可验证模型，但 manifest 的系统/架构/profile 声明不是浏览器或实机验收证据。受信任进程拥有自身 OS 权限，当前协议不声称隔离恶意插件。
