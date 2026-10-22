# 插件控制协议 1.0

## 帧与握手

官方 Worker 与 Host 使用 JSON-RPC 2.0。每帧为 `Content-Length: <UTF-8 正文字节数>\r\n\r\n` 后跟 JSON 正文；单帧最多 1 MiB，每页最多 200 项。stdout 只承载帧，英文诊断写 stderr；大块音频、PCM、图片与文件正文不能进入 JSON/Base64。

Host 发出的 ID 为 `h-` 前缀，Worker 调用 Host 时用 `w-`。一帧只承载一个请求、响应或取消通知，不支持 batch。干净 EOF 表示对端结束；标头/正文半帧 EOF 是协议失败。必须设置握手及请求 deadline，读取取消/超时由关闭受控通道传播，不无限等待部分帧。

`host.hello` 请求绑定 instanceId、配置修订和本次随机 nonce。成功结果为 Ready（逻辑上的 worker.ready），必须回显 instanceId、nonce，并用 manifest 声明精确 protocolVersion。1.0 只协商 1.0；主版本不匹配必须拒绝。每个角色实例独立 Worker，实例/账号权限由 Host 启动上下文固定，不能被请求字段切换。账户引用不是凭据。

Manifest 的 `pluginVersion` 对 Provider、音频和 Scraper 都是必填项，必须是最多 128 字符的有效 SemVer；缺失、空白、不完整版本及带前导零的纯数字预发行标识均被拒绝。`pluginVersion` 是插件发行版本，独立于 `protocolVersion=1.0`、Server 版本和配置修订；发行号有效不代表协议兼容，角色的其他握手约束仍须满足。随包 Worker 与宿主元数据共用 [sdk/bundledplugin/releases.go](../../../../sdk/bundledplugin/releases.go) 的各插件声明。

`GET /api/v1/admin/plugins` 的插件项 `version` 同样必填；尚无有效握手版本时使用随包声明，覆盖没有实例、未启动、待配置、已禁用或启动失败的状态，有效握手版本优先。该展示字段不授予运行能力，也不以宿主版本填充。独立 DSP 和存储采用专用握手，同样要求必填 `pluginVersion`；新宿主拒绝缺少发行号的旧 Worker，须配套升级，具体见 [DSP 插件](../../../plugins/dsp-plugins.md)与[存储插件](../../../plugins/storage-plugins.md)。

## 身份、能力与展示

`instanceId` 使用规范小写、非空 UUID，与插件 ID 和显示别名分离。Manifest 必须包含 `plugin`（`id/name/localizedNames/features`）与 `supportsMultipleInstances`；`name` 是插件提供的默认别名，`localizedNames` 提供语言别名。SDK 的多实例布尔值默认 `false`，官方在线来源和本地元数据提取声明 `true`，本地音乐、FFmpeg、NCM 声明 `false`。单实例插件最多保存一个实例，禁用实例同样计数。宿主核验握手声明，管理接口 `/admin/plugins` 对每个插件返回同名字段，客户端据此决定是否展示多实例入口。在线实例名称优先使用当前用户已验证的平台账号用户名，退出登录后不保留账号名，也不以 UUID 替代显示别名。

FFmpeg/NCM 的单次任务握手使用 `DecoderHello`，额外必需 `taskId`。实例 UUID 在该配置生命周期内保持稳定；请求的任务 ID 必须匹配握手任务 ID，nonce 继续绑定本次通道。本地音乐和在线来源握手不传此字段。

在线 Provider 的资料修订为 `online-authenticated-v3`，`provider.online.describe` 必须返回插件提供的 `plugin: {id,name,localizedNames,features}`，其 ID 与握手身份一致；Host 缓存并透传已验证值，不自行生成平台名称。显示语义、生命周期与版本兼容见 [插件身份与名称](../../../plugins/plugin-identity.md)，具体 wire 模型见 [online.schema.json](online.schema.json)。

音乐来源可提供独立的展示统计 `{value, unit, localizedUnits}`；在线来源的数字、默认单位与译名由插件负责。本地音乐的管理摘要由宿主按有效部署权限生成允许访问目录数，这是权限展示的例外；包含零值、待配置与未启动状态，不使用已添加目录数量，也不向 Worker 传递未选授权路径。`value` 是 0—9007199254740991 的整数，`unit` 及每项译名是 1—128 个 Unicode 字符的纯文本；语言标签与身份译名采用相同校验，`localizedUnits` 必需且可为空对象。统计不能代替实例身份或权限。Web 不显示实例数量；插件上报的“个用户”“个来源”等单位也不由前端固定映射。

兼容接口 `plugin.displayCount` 接受 `{instanceId, rootIds}`，仅传当前选中根 ID 并返回旧的来源统计，保留进程协议兼容。当前管理列表不使用这项统计，而由宿主返回允许访问目录数及“个允许访问的目录”译名，新增或移除已添加目录不影响权限数量。在线来源仍通过[账号响应](account.md)的可选 `displayCount` 提供统计；管理查询读取缓存、不触发 RPC。此展示字段不增加目录访问能力或账号权限。

Go 消费入口位于独立 `sdk` 模块，见 [SDK 使用与分发](../../../plugins/plugin-sdk.md)。在线插件的 `pluginVersion` 接受合法语义版本；通信版本与资料修订仍精确校验。能力可声明当前平台允许集合的子集，Manifest 启用能力须与描述中的 `supported=true` 集合一致。宿主保留对未知或未验证能力的拒绝。平台账号作用域保持不变；插件管理 `version` 必填规则见前文。

## Local Files 配置与扫描

Manifest 的 `configuration` 为插件提供的导入选项声明；不支持导入配置的音频插件发送null。Local Files 0.3.0声明 `local-import-v5`，字段为 `schemaRevision/importOptionsSchema/importDefaults/unavailableOptions`，标题与说明为纯文本。声明限制32 KiB、8层、每对象32属性；受限Schema支持有界字符串数组，拒绝外部引用/执行扩展。`ImportOptions` 和 `ScanScope` 保留必需 `excludePatterns` 数组，最多32条、每条UTF-8最多256字节；Host与发布范围共用根相对glob语义，见[扫描说明](../../../library/scanning.md)。规则排序绑定PlanDigest，cursor/grant不能在同任务中换规则。

`ImportOptions`、`ImportOverrides` 与 `ScanScope` 包含可选布尔字段 `skipHidden` 和 `respectNoMedia`。新实例默认均为 `true`，分别跳过点开头及 Windows 隐藏属性条目、跳过含 `.nomedia` 的目录及子树。旧持久计划省略时继续按 `false/false` 解释，旧摘要保持不变；配置声明的 `importDefaults` 则总是显式发送两个字段，包含 `false`。

宿主实际执行根授权、递归上限和排除，不依赖worker自行过滤后声称完整。Host 将隐藏内容与 `.nomedia` 的动态排除附加到内部发布证据，并校验 Worker 返回页与该 Host 枚举页一致；排除证据不增加 wire 字段，Worker 无权声明。数据库对账保留被排除范围的旧记录。`importLyrics:true` 已接入扫描后的持久歌词补缺，需要可用本地Scraper；逐目标失败不伪装成功，见[自动补缺](../../../metadata/metadata.md#导入歌词与自动补缺)。

`provider.scan` 携带 scope（rootId/rootIdentity/recursive/maxDepth）、planDigest 和 Host 生成的单页 grantId。Worker 调用 `host.entries` 必须携带该 grantId；授权绑定任务、代次、范围和游标，不能跨任务、根或选项重用。返回页包含 scanId/generation/page/planDigest，末页 complete 还须结合 issues 和根身份决定是否可以发布完整请求范围。recursive=true 且 maxDepth=0 表示不限深度，不能映射为64；recursive=false/maxDepth=0 只覆盖根本层，正数深度1—64保持既有语义。Host目录遍历、目录监听和数据库缺失对账使用同一范围规则；范围外曲目不能被标记缺失。Entry 的 fileIdentity 为原始身份，linkCount 为实际硬链接数，不能按本轮枚举数量改写身份。持久来源身份由 Host 命名空间状态维护。`provider.resolve` 返回受限资源引用，rootId/relativePath 只在受信任控制面使用，浏览器看不到它们；Host 实际打开时仍需验证根、版本和授权。

本地作品包使用 `provider.observePackage` 精确观察接口，请求为 `PackageObserveRequest{entries: Entry[]}`，结果为 `PackageObserveResult{items: Observation[]}`。它仅适用于已握手的 Local Files 实例，宿主核对当前所有者、实例读租约、授权根、根身份和配置修订后，将完整包快照中经音频插件实际内容检测的音频条目发送给插件。每批 1—200 项，请求参数 JSON 最多 349525 字节；宿主按编码字节预算拆批，为返回名称和 JSON-RPC 信封保留空间。结果必须等长、同序，逐项匹配根、相对路径、版本、大小和 MIME；`sourceRef` 必须等于当前 `externalId`，批次和整包均不能出现重复身份。插件沿用常规扫描的文件身份、硬链接规则及 `track-{externalId}` 状态，后续仍通过现有 `provider.resolve` 播放。

作品包枚举由宿主完成，插件不得扩展请求、调用目录扫描补充条目、凭相对路径读文件或发布数据库记录。宿主在调用前后复核条目并重新比较整个包快照，必要的音轨元数据探测使用宿主受权读句柄。包仅代表选定目录，观察结果没有 `complete` 或扫描覆盖凭据，**不能调用完整扫描 Publish，也不能将包外曲目标记为缺失**；持久化必须使用作品包的精确新增或更新事务。普通扫描依旧使用 `provider.scan` 及其独立范围凭据。

宿主对包读取实行 30 秒期限、5000 个枚举节点（含遍历中遇到的排除项）、32 层深度、累计 2 MiB 相对路径字节和单路径 4096 字节上限，超限或读取不完整直接失败。目录选择一次最多返回 1000 个子目录，并限制检查条目数量；不截断结果。包保留全部有效目录树，忽略常规扫描 `recursive/maxDepth` 裁剪，但遵守当前根的隐藏项、`.nomedia` 和排除规则。拒绝绝对路径、盘符、反斜杠、父目录、非规范路径、链接祖先及遇到的链接条目；文件使用版本引用，根与包内每个目录身份纳入快照。附件正文不进入控制协议，受权稳定读取绑定实例租约，取消实例操作会关闭读取保护。当前仅 PNG/JPEG/GIF 可归为图片预览，普通文本归为文本，其余文件（含 WebP）仅供下载。

观测字段中必需但可为 `null` 的 `bitrateMode` 与 `providerSha512`：模式仅接受 `cbr/vbr/unknown`，摘要必须是 128 个小写十六进制字符。`bitrateBps` 仅接受正整数或 `null`，质量档位不能作为数值。Provider 的摘要与码率属于申报证据，不能自行声明 `verified` 或选择 `bitrateOrigin`；Host 单独记录其实际探测来源，并将证据绑定到当前资产、版本、扫描代次和算法配置。未知值须显式发送 `null`。

`track.details` 能力使用 `provider.trackDetails`，请求为 `ResolveRequest`，结果为插件提供的 `TrackDetails` 有序纯文本分组。在线版本使用 `provider.online.trackDetails` 与 `OnlineReference`。宿主只在曲目所有者和来源实例权限校验后调用；本地详情额外校验当前文件版本。此详情接口允许用户查看自己来源内的相对路径和根标识，仍不暴露服务器绝对路径。结构、上限与错误语义见[来源曲目详情](../../../library/source-track-details.md)。

## 取消、错误与 Host 服务

`$/cancelRequest` 是无 ID 通知，其 params.id 指向在途调用。已返回后取消幂等；已取消的晚结果只可回收，不可发布。重复终态、重复活跃 ID、未知响应 ID 都视为协议错误。取消不是强制 OS 沙箱；拒绝退出的受管 Worker 由 Host 在宽限期后回收。

JSON-RPC 数值码：-32700 解析错误、-32600 非法请求、-32601 未知方法、-32602 非法参数、-32603 内部协议错误；业务失败统一 -32000，并在 data 给出稳定 code/retryable/retryAfterMs。明确不支持的已识别能力返回 `UNSUPPORTED`。不能以空成功响应代替未实现功能。

Local Files的文件通知由宿主按实例 `config.watch` 调度，不新增Worker自行遍历或提交任意路径的能力。通知只产生合并扫描提示，仍使用当前声明、已授权根、实例默认范围和新的受限任务授权。通知丢失/不可用以周期完整复核补漏；活动或重试等待中的任务不会被事件洪峰重复替换，paused停止自动提交。协议中的增量扫描仍须完整覆盖声明scope才可发布；不得把通知列表冒充完成观察。目录监听与资源上限见[扫描说明](../../../library/scanning.md)。

Host 的 `host.entries` 返回固定根身份的 Entry 页；`host.state.get/compareAndSet` 使用实例隔离的 key/revision/value。value 是限额 512 KiB 的状态字节（Go `[]byte` 的 Base64 JSON 编码），仅保存身份等小型状态，不能承载音频正文。`host.resource` 由 Host 检查受限相对路径后分配不透明引用；Worker 不自行签发引用。

## 音频处理

音频 Worker 的 hello 额外接收已由 Host 探测的 capabilities。`audio.describe` 返回实际工具摘要、profile 和 PCM；`audio.prepare/start` 接收固定计划与 loopback input/output 任务通道。音频二进制由任务限定的 loopback HTTP 传输，不写入控制面。宿主注入的 FFmpeg 绝对路径和可执行文件摘要仅在受信任配置边界使用，不发送给 Web。取消仍使用统一取消通知；具体字段是插件内的 `gainDB`，HTTP 用户偏好是 `gainDb`，两者在边界显式转换。

`audio.prepare/start` 还可接收 `dspChain`（最多 15 个 `{name,version:1,params}` 节点）与 `dspPlugins`（最多 16 个外部可执行节点注册配置）。`disableEffects=true` 时跳过用户 gain、全部效果与外部节点，仅保留编解码及按开关执行的声道转换；`disableChannelMix=true` 进一步禁止声道插件和其历史链节点，宿主按来源选择输出档案，必要编码兼容适配由 FFmpeg 完成；否则用户 gain 位于首位，配置链按数组顺序执行。内建 `gain` 接受唯一的 `gainDB` 参数，范围 -96—12 dB；`bypass` 不接受参数。外部节点配置包含 `name/executable/sha256` 和可选 `args/timeoutMs`，宿主解析绝对路径并验证摘要，名称不能覆盖内建节点；这些执行配置属于受信任部署边界，不发送给 Web。

官方 FFmpeg Worker 实现版本为 `0.7.0`，Host 对其 pluginId 与 pluginVersion 作精确匹配。旧版本在发送音频请求前被拒绝，不因配置链为空而放宽。`plan.profile` 包含实际目标码率：MP3 立体声使用 `mp3-<kbit/s>`、单声道使用 `mp3-mono-<kbit/s>`，Opus 的非默认目标使用 `opus-<布局>-<bit/s>`；Host 与 Worker 从同一 ID 解析确切编码参数，动态 Opus ID 的码率后缀必须等于描述中的 `bitrateBps`。范围与默认值见[音频处理](../../../playback/audio-processing.md)。内置 equalizer/compressor/reverb 支持有状态及多声道处理，定位从曲首重建后裁切；声道转换位于效果链前，具体见[效果与布局](../../../plugins/dsp-effects.md)。Server 与显式外部音频 Worker 必须配套升级；其他 Provider/NCM/Scraper 的 Worker 版本不因本扩展改变。

`plan.decodeOutput` 可省略以沿用 `pcm_f32le`，也可指定 `pcm_s16le`。实际解码输出使用 WAV 携带原声道掩码，整数 PCM 在进入 DSP 前适配为 float32；此字段不改变播放编码 profile。`capabilities.decodeOutputs` 声明实际可用的 PCM 输出，`transcodeFormats` 声明显式转码支持的格式。`audio.decode` 与 `audio.transcode` 使用 `AudioCodecRequest`：前者只接受两种 PCM，后者接受 MP3、FLAC、Opus/WebM 或 WAV；两者均不执行播放 DSP，正文通过任务通道传输。`maxOutputBytes` 在宿主与 Worker 两端校验，上限为 1 GiB，SDK 调用省略时为 256 MiB；随机读取结果由任务临时文件承载，关闭、取消或错误时回收，不能改写来源文件。完整链式语义见[音频编解码插件](../../../plugins/audio-codecs.md)。

外部 DSP 使用独立的 [DSP v1 二进制帧协议](../../../plugins/dsp-plugins.md)，不是 JSON-RPC 子方法；PCM 不进入控制协议。外部节点仍必须声明并遵守 `f32le/48000/stereo`、无状态、保持帧数和时标、零延迟与无追加尾音；多声道输出拒绝使用这些节点。新内置效果使用直接 Go 接口，不改变外部帧协议。宿主检查握手、身份、有限数值和时标，超时或取消回收进程；任意图、动态库加载与自动旁路仍不支持。

Capabilities 的 `normalization` 明确报告是否具备指纹 PCM 规范化能力。`audio.normalize` 使用单独计划 `taskId/assetId/startMs/durationMs` 与宿主限定的输入、输出通道；起点范围为0—86400000毫秒，请求长度10000—120000毫秒，拒绝增益或其他 DSP 参数。输出固定为11025 Hz、单声道、s16le，规范化版本为 `s16le-mono-11025-floor-range-v1`，两端点按绝对样本位置向下量化。控制响应只含实际格式、样本起点/数量和解码器版本/摘要，不含 PCM 字节；宿主复核实际字节数、范围和工具身份。尾部不足请求长度不能补零扩窗，后续指纹质量判断与实际覆盖分开记录。

## NCM

独立 NCM Worker 使用 `musicfs.plugin.decoder.ncm` 标识、`audio` 角色与 `audio.decrypt` 能力，不依赖 FFmpeg。`host.hello` 的 instanceId 为配置实例 UUID，configRevision 为 `1`；nonce 是 24 个随机字节的 48 位小写十六进制串。一个 Worker 只接受一次 `ncm.open`，绑定 taskId、assetId、nonce、inputUrl 和加密输入 size，拒绝重新绑定。返回同一任务身份、outputUrl、解密后的 size 与 metadata；format/MIME 由解密正文确认，仅支持 MP3/FLAC，未知标签省略，不下载封面或任何外部资源。请求中没有源路径、原始音频或密钥。

NCM 的 input/output 通道均只监听 `127.0.0.1` 随机端口，路径包含本次 nonce；接收方校验完整任务绑定及精确 URL。只允许单段显式 `bytes=start-end` GET，每段最多 64 KiB；返回必须为 `206` 且 Content-Range 的起止/总长、Content-Length 完全一致，禁止重定向、代理、压缩响应及其他地址。通道最多接受 8 个连接、4 个并发请求，设定标头、读写及空闲超时。内置实现固定于受信任 Server 构建；显式外部模式由 Host 校验 Worker SHA-256，每次打开也复核；默认同时最多 2 个任务，最多可配置 8 个。启动及打开共限 5 秒，单任务最长 2 分钟。上层须在生命周期内读取或拷贝解密内容；取消与 Close 回收控制管道、两个 HTTP 服务及任务上下文，只有显式外部模式回收插件进程，不保留可复用输入能力。

## 本地 Scraper

独立 `musicfs.scraper.local` 使用 `scraper` 角色和 `scrape.search/scrape.fetch/lyrics/artwork` 声明。专用 `host.hello` 固定实例、owner/task上下文、来源/资产版本、媒体修订及一次性 loopback baseUrl。一次会话最多30秒，只能搜索一次；`scrape.search` 接受已授权资源描述、字段、语言和可选查询信息，返回最多64个候选及问题。候选以字段、声明语言、格式、大小、SHA-256、embedded/sidecar来源依据和资源版本描述内容，不含正文。当前wire保留原声明语言，业务层将常见ID3别名规范为主语言，封面语言归空，并明确拒绝不支持的非空查询修正。

`scrape.fetch` 只能提取同会话已搜索的候选，目标及输出引用必须匹配。Host 提供最多32个资源，资源/输出引用均为48位小写随机十六进制标识，不是路径；旁置输入总预算32 MiB，音频仅通过受限读取器访问。正文走本次nonce限定的 `127.0.0.1` 二进制通道，每段读取最多64 KiB，不经控制JSON/Base64。禁止代理、重定向和插件指定路径，宿主二次校验实际格式、长度、摘要及源稳定状态。公开HTTP候选移除内部资源引用/版本，预览内容交由MediaStore权限管理；插件本身不能选择内容、写原文件或合并录音。

本版本已定义首个 Provider、Host 状态/资源、音频处理、NCM 解密和本地 Scraper 的具体 wire 模型。Schema 可验证模型，但跨字段 nonce 相等、端口范围、输入/输出长度关系和生命周期由运行时继续检查。manifest 的系统/架构/profile 声明不是浏览器或实机验收证据。受信任进程拥有自身 OS 权限，当前协议不隔离恶意插件。

## 本地插件配置持久化

Local Files 目录与导入/监听设置通过管理 API 持久化到 Server 的插件配置数据库；YAML 的实例 permissions.readDirectories 限定目录访问范围。宿主提供按实例隔离的 host.database.get/compareAndSet，受管目录设置仍经权限检查与排空重建。具体接口、旧配置兼容及客户端影响见[插件配置数据库与目录管理](../../../plugins/plugin-configuration.md)。

宿主持久扫描计划 `ScanPlan` 支持可选 `rootOptions` 映射，键限本次所选根 ID，值为该根完整有效导入选项；优先级为插件默认、实例默认、目录覆盖、本次请求覆盖。目录覆盖包含递归、深度、隐藏项、`.nomedia`、排除规则和歌词导入。计划摘要绑定目录选项，扫描枚举、监听目录、发布清理及歌词触发均使用目标目录的有效值。省略映射的旧计划含义及摘要不变；Worker 仍接收现有按根 `ScanScope`，不增加物理路径或权限。新增计划须由更新后的宿主恢复，不能降级为忽略目录选项的旧宿主。任务响应以可选 `effectiveRootOptions` 公开这些目录选项，未列出的目录使用 `effectiveOptions`。
