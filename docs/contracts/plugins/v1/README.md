# 插件控制协议 1.0

官方 Worker 与 Host 使用 JSON-RPC 2.0。每帧为 `Content-Length: <UTF-8 正文字节数>\r\n\r\n` 后跟 JSON 正文；单帧最多 1 MiB，每页最多 200 项。stdout 只承载帧，英文诊断写 stderr；大块音频、PCM、图片与文件正文不能进入 JSON/Base64。

Host 发出的 ID 为 `h-` 前缀，Worker 调用 Host 时用 `w-`。一帧只承载一个请求、响应或取消通知，不支持 batch。干净 EOF 表示对端结束；标头/正文半帧 EOF 是协议失败。必须设置握手及请求 deadline，读取取消/超时由关闭受控通道传播，不无限等待部分帧。

`host.hello` 请求绑定 instanceId、配置修订和本次随机 nonce。成功结果为 Ready（逻辑上的 worker.ready），必须回显 instanceId、nonce，并用 manifest 声明精确 protocolVersion。1.0 只协商 1.0；主版本不匹配必须拒绝。每个角色实例独立 Worker，实例/账号权限由 Host 启动上下文固定，不能被请求字段切换。账户引用不是凭据。

Manifest 的 `configuration` 为插件提供的导入选项声明；不支持导入配置的音频插件发送 null。Local Files 声明 `schemaRevision/importOptionsSchema/importDefaults/unavailableOptions`，标题与说明是纯文本。声明限制为 32 KiB、8 层、每对象 32 个属性及有限 JSON Schema 关键字，拒绝引用与可执行扩展。宿主共用声明校验实例默认和单次请求，但根授权、递归深度上限与真实遍历限制始终由宿主执行。歌词选项尚未实现，true 返回 `UNSUPPORTED`。

`provider.scan` 携带 scope（rootId/rootIdentity/recursive/maxDepth）、planDigest 和 Host 生成的单页 grantId。Worker 调用 `host.entries` 必须携带该 grantId；授权绑定任务、代次、范围和游标，不能跨任务、根或选项重用。返回页包含 scanId/generation/page/planDigest，末页 complete 还须结合 issues 和根身份决定是否可以发布完整请求范围。范围外曲目不能被标记缺失。Entry 的 fileIdentity 为原始身份，linkCount 为实际硬链接数，不能按本轮枚举数量改写身份。持久来源身份由 Host 命名空间状态维护。`provider.resolve` 返回受限资源引用，rootId/relativePath 只在受信任控制面使用，浏览器看不到它们；Host 实际打开时仍需验证根、版本和授权。

S-P5-01 观测增加必需但可为 `null` 的 `bitrateMode` 与 `providerSha512`：模式仅接受 `cbr/vbr/unknown`，摘要必须是 128 个小写十六进制字符。`bitrateBps` 仅接受正整数或 `null`，质量档位不能作为数值。Provider 的摘要与码率属于申报证据，不能自行声明 `verified` 或选择 `bitrateOrigin`；Host 单独记录其实际探测来源，并将证据绑定到当前资产、版本、扫描代次和算法配置。尚未上线的 v1 开发基线直接要求新字段，未知值须显式发送 `null`。

`$/cancelRequest` 是无 ID 通知，其 params.id 指向在途调用。已返回后取消幂等；已取消的晚结果只可回收，不可发布。重复终态、重复活跃 ID、未知响应 ID 都视为协议错误。取消不是强制 OS 沙箱；拒绝退出的受管 Worker 由 Host 在宽限期后回收。

JSON-RPC 数值码：-32700 解析错误、-32600 非法请求、-32601 未知方法、-32602 非法参数、-32603 内部协议错误；业务失败统一 -32000，并在 data 给出稳定 code/retryable/retryAfterMs。明确不支持的已识别能力返回 `UNSUPPORTED`。不能以空成功响应代替未实现功能。

Host 的 `host.entries` 返回固定根身份的 Entry 页；`host.state.get/compareAndSet` 使用实例隔离的 key/revision/value。value 是限额 512 KiB 的状态字节（Go `[]byte` 的 Base64 JSON 编码），仅保存身份等小型状态，不能承载音频正文。`host.resource` 由 Host 检查受限相对路径后分配不透明引用；Worker 不自行签发引用。

音频 Worker 的 hello 额外接收已由 Host 探测的 capabilities。`audio.describe` 返回实际工具摘要、profile 和 PCM；`audio.prepare/start` 接收固定计划与 loopback input/output 任务通道。音频二进制由任务限定的 loopback HTTP 传输，不写入控制面。宿主注入的 FFmpeg 绝对路径和可执行文件摘要仅在受信任配置边界使用，不发送给 Web。取消仍使用统一取消通知；具体字段是插件内的 `gainDB`，HTTP 用户偏好是 `gainDb`，两者在边界显式转换。

Capabilities 的 `normalization` 明确报告是否具备指纹 PCM 规范化能力。`audio.normalize` 使用单独计划 `taskId/assetId/startMs/durationMs` 与宿主限定的输入、输出通道；起点范围为0—86400000毫秒，请求长度10000—120000毫秒，拒绝增益或其他 DSP 参数。输出固定为11025 Hz、单声道、s16le，规范化版本为 `s16le-mono-11025-floor-range-v1`，两端点按绝对样本位置向下量化。控制响应只含实际格式、样本起点/数量和解码器版本/摘要，不含 PCM 字节；宿主复核实际字节数、范围和工具身份。尾部不足请求长度不能补零扩窗，后续指纹质量判断与实际覆盖分开记录。

独立 NCM Worker 使用 `musicfs.ncm` 标识、`audio` 角色与 `audio.decrypt` 能力，不依赖 FFmpeg。`host.hello` 的 instanceId 固定为 `ncm-<taskId>`，configRevision 为 `1`；nonce 是 24 个随机字节的 48 位小写十六进制串。一个 Worker 只接受一次 `ncm.open`，绑定 taskId、assetId、nonce、inputUrl 和加密输入 size，拒绝重新绑定。返回同一任务身份、outputUrl、解密后的 size 与 metadata；format/MIME 由解密正文确认，仅支持 MP3/FLAC，未知标签省略，不下载封面或任何外部资源。请求中没有源路径、原始音频或密钥。

NCM 的 input/output 通道均只监听 `127.0.0.1` 随机端口，路径包含本次 nonce；接收方校验完整任务绑定及精确 URL。只允许单段显式 `bytes=start-end` GET，每段最多 64 KiB；返回必须为 `206` 且 Content-Range 的起止/总长、Content-Length 完全一致，禁止重定向、代理、压缩响应及其他地址。通道最多接受 8 个连接、4 个并发请求，设定标头、读写及空闲超时。Host 校验 Worker SHA-256，每次打开也复核；默认同时最多 2 个任务，最多可配置 8 个。启动及打开共限 5 秒，单任务最长 2 分钟。上层须在生命周期内读取或拷贝解密内容；取消与 Close 回收控制管道、两个 HTTP 服务和进程，不保留可复用输入能力。

独立 `musicfs.scraper.local` 使用 `scraper` 角色和 `scrape.search/scrape.fetch/lyrics/artwork` 声明。专用 `host.hello` 固定实例、owner/task上下文、来源/资产版本、媒体修订及一次性 loopback baseUrl。一次会话最多30秒，只能搜索一次；`scrape.search` 接受已授权资源描述、字段、语言和可选查询信息，返回最多64个候选及问题。候选以字段、声明语言、格式、大小、SHA-256、embedded/sidecar来源依据和资源版本描述内容，不含正文。当前wire保留原声明语言，业务层将常见ID3别名规范为主语言，封面语言归空，并明确拒绝不支持的非空查询修正。

`scrape.fetch` 只能提取同会话已搜索的候选，目标及输出引用必须匹配。Host 提供最多32个资源，资源/输出引用均为48位小写随机十六进制标识，不是路径；旁置输入总预算32 MiB，音频仅通过受限读取器访问。正文走本次nonce限定的 `127.0.0.1` 二进制通道，每段读取最多64 KiB，不经控制JSON/Base64。禁止代理、重定向和插件指定路径，宿主二次校验实际格式、长度、摘要及源稳定状态。公开HTTP候选移除内部资源引用/版本，预览内容交由MediaStore权限管理；插件本身不能选择内容、写原文件或合并录音。

本版本已定义首个 Provider、Host 状态/资源、音频处理、NCM 解密和本地 Scraper 的具体 wire 模型。Schema 可验证模型，但跨字段 nonce 相等、端口范围、输入/输出长度关系和生命周期由运行时继续检查。manifest 的系统/架构/profile 声明不是浏览器或实机验收证据。受信任进程拥有自身 OS 权限，当前协议不声称隔离恶意插件。
