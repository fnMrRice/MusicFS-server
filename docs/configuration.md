# Server YAML 配置

状态：2026-10-09，配置解析、显式插件/实例模型及带修订的读写服务已实现。运行时动态启停、管理 API/命令、排空与强制停止仍属[后续插件管理](implementation/plugin-management.md)。本版本只使用 YAML，不兼容旧 JSON 配置，不提供旧字段回退或自动转换。

启动见[运行说明](running.md)；正式[示例配置](../configs/server.example.yaml)和[编辑器 Schema](../configs/server.schema.json)随代码维护，实际验证见[配置验收](validation/configuration.md)。HTTP JSON、JSON-RPC、JSON Schema 和媒体清单继续使用各自格式。

## 1. 文件与严格校验

配置路径必须为 `.yaml` / `.yml`，建议复制示例为 `configs/server.local.yaml` 后编辑。只接受一个顶层 mapping 文档，`configVersion` 必须为整数 `1`；大小为1字节至1 MiB，嵌套最多32层。解析器拒绝未知字段、重复键、额外文档、anchor/alias、merge、自定义标签、null及不匹配的类型。

布尔值只接受未加引号的小写 `true` / `false`；`"false"`、`yes`、`on`不替代布尔值。整数须为规范十进制，不接受浮点、十六进制、前导零或数值字符串。允许中文、注释、空格及带反斜线的Windows路径；Windows路径建议用单引号，如 `'D:\Music Files'`。语法和字段错误只返回位置与安全说明，不回显原始敏感值。

相对路径以配置文件所在目录为基准，包括默认目录以及环境/命令行覆盖的相对路径。`dataRoot`、`mediaRoot`、`cacheRoot` 必须彼此分离，音乐根不得与任一可写根重叠。数据库仍放本地文件系统；音乐可来自已挂载目录。

## 2. 服务字段与覆盖

| 字段 | 默认值与约束 |
| --- | --- |
| `configVersion` | 必填，当前仅1 |
| `listen` | `127.0.0.1:8080`；合法监听地址和1—65535端口 |
| `dataRoot`、`mediaRoot`、`cacheRoot` | `data`、`media`、`cache`，以配置目录解析，须非空且分离 |
| `webRoot` | 可选构建制品目录；也可通过CLI `--web-root`指定 |
| `allowedOrigins` | 必须显式列出页面完整Origin，无路径、查询、片段或用户信息 |
| `allowLoopbackHttp` | 默认false；只有IP字面量loopback可显式使用HTTP，远程监听须TLS |
| `tlsCert`、`tlsKey` | 同时配置；HTTPS部署还须匹配允许的Origin |
| `maxSessions` | 默认8，范围1—128 |
| `maxProcessing` | 默认2，范围1—8 |
| `maxCacheBytes` | 默认1073741824，至少1048576，最大为有符号64位整数 |
| `sessionTtlSeconds` | 默认3600，范围60—86400 |
| `hashing.auto` | 默认false；是否自动排队完整原文件 SHA-512，关闭不影响手动任务 |
| `hashing.maxConcurrent` | 默认1，范围1—4；后台哈希任务并发上限 |
| `hashing.bytesPerSecond` | 默认4194304（4 MiB/s），范围65536—1073741824；所有哈希任务共享的读取预算，不是每任务额度 |
| `fingerprinting.enabled` | 默认false；是否启用本实例的手动片段指纹任务 |
| `fingerprinting.fpcalcPath` | 默认空；启用时必填，外部 Chromaprint 1.6.1 的 fpcalc 路径，以配置目录为相对基准 |
| `fingerprinting.fpcalcSha256` | 默认空；启用时必填64位十六进制摘要，加载后转为小写；不能仅信任程序文件名 |
| `fingerprinting.maxConcurrent` | 默认1，范围1—2；指纹任务并发上限；DSP 前规范化和 NCM 解密各有独立的后台1槽位，不占前台播放准入槽位 |
| `fingerprinting.timeoutSeconds` | 默认30，范围1—120；指纹任务时间上限；已有规范化管线另有30秒保护，上层较长配置不会撤销内层限制 |
| `plugins` | 显式插件列表，可为空；模型见下一节 |
| `writeBack.enabled` | 默认false；显式原件写回开关，不由上传/选用/自动补缺开启 |
| `writeBack.allowOverwrite` | 默认false；覆盖旧标签还要求本次请求显式overwrite |
| `writeBack.maxFileBytes` | 默认8589934592（8 GiB），范围46—1099511627776（1 TiB） |
| `writeBack.roots` | 默认空；最多256个不重复的`{instanceId,rootId,fields}`授权，字段限lyrics/artwork |

优先级为内置默认值 → YAML → 支持的环境变量 → 显式CLI参数。支持 `MUSICFS_LISTEN`、`MUSICFS_DATA_ROOT`、`MUSICFS_MEDIA_ROOT`、`MUSICFS_CACHE_ROOT`、`MUSICFS_WEB_ROOT` 和 `MUSICFS_ADMIN_TOKEN`；当前CLI仅为普通配置提供 `--web-root` 覆盖，`--config`选择文件。插件及实例开关不接受环境或CLI覆盖。

管理员令牌至少24字符，只通过 `MUSICFS_ADMIN_TOKEN` 注入，不是YAML字段。读写服务的 `Snapshot` 清空该值；凭据、环境变量和CLI生效值不写回文件。不要把凭据放入URL、长期测试材料或Git。

`hashing` 可省略或只覆盖部分字段；其默认值会逐项补齐，显式0、超出上限、未知子字段或类型错误均拒绝。哈希使用独立后台队列和上述共享预算，默认自动关闭，以减少启动和扫描后的磁盘竞争。手动创建任务仍受归属、稳定完整读取能力及资源上限约束；它计算包含容器、标签、内嵌封面的原始文件字节，不能当作解码 PCM 或录音相同性证明。修改这些配置后需重启。

`fingerprinting` 同样允许省略或部分覆盖，显式0、null、未知键或类型错误拒绝。启用时同时要求 `musicfs.audio.ffmpeg` 插件和一个音频实例明确启用，复用其 DSP 前规范化能力；不会自动打开音频插件。fpcalc 运行时须校验指定摘要及精确版本 1.6.1，算法固定为 2，不允许请求自选工具路径、算法或版本。禁用时可省略工具路径和摘要，不启动 fpcalc。没有 `auto`、上传、外部识别或远程检索配置项。

本步只提供最长120秒的手动片段请求：起点0—86400000毫秒，请求时长10000—120000毫秒。实际读到的尾段可以更短，须如实记录覆盖样本数和 `short` 质量，不能补零冒充完整片段或当作录音匹配依据。工具不可用或摘要/版本不符时只使指纹能力返回 `UNSUPPORTED`，不使整个服务和普通播放退出。创建、取消和按所有者读取明细见[HTTP 契约](contracts/README.md#s-p5-03-本地片段指纹)，实测范围见[指纹接口验收](validation/fingerprinting-http.md)。

## 3. 插件与实例

每个插件包含 `pluginId`、`executable`、可选64位十六进制 `sha256`、`enabled` 和 `instances`。每个实例包含全局唯一 `instanceId`、可选 `name`、`role`、`enabled` 与 `config`。实例ID和音乐根ID由1—128个ASCII字母、数字、连字符或下划线组成；身份不由数组位置或显示名生成。实例ID忽略大小写后也不得重复，以免在Windows上共用状态目录；单个ID的原始大小写保持不变。

| `pluginId` | 角色与实例参数 |
| --- | --- |
| `musicfs.localfiles` | `provider`；`config.musicRoots`包含`id/name/path`。有效启用时至少一个音乐根；根ID在该实例内唯一 |
| `musicfs.audio.ffmpeg` | `audio`；`config.ffmpegPath`与`config.ffprobePath`。有效启用时两者均必填 |
| `musicfs.ncm` | `audio`；`config: {}`，具体功能见[NCM说明](ncm.md) |
| `musicfs.scraper.local` | `scraper`；`config: {}`，最多16个配置实例，只读取目标Provider授权的内嵌/旁置内容；见[补全任务](metadata.md) |

当前只接受上述四个官方插件ID，插件ID不可重复；每个音频插件最多有一个有效启用实例。插件与实例的 `enabled` 省略时均默认false，只有两层都为true才在启动时启用。禁用不改写兄弟实例或删除已有库。有效启用必须配置可执行路径；禁用对象仍严格检查结构、身份、路径隔离与摘要格式，但不要求其程序或外部工具当前可运行。

示例显式启用Local Files、NCM与本地Scraper；音频插件和实例默认false。需要转码/DSP时，填写音频实例的两个工具路径，并将两层 `enabled` 都改为true后重启。Linux将`executable`改为对应Linux构建路径，程序无`.exe`后缀。可选`sha256`用于钉住管理员审核的程序；省略时采用现有显式本机路径信任与本次启动摘要检查，不代表跨重启钉住版本。

### 导入选项

Local Files 实例可在 `config` 同级声明 `importDefaults`。这些字段的标题、说明、默认值与校验规则由官方插件的 `Manifest.configuration` 提供；宿主加载器共用该声明，实际遍历还受宿主的根授权和深度上限约束。每个实例最多授权 256 个根，客户端只能选择已授权根编号。

```yaml
importDefaults:
  recursive: true
  maxDepth: 64
  importLyrics: false
```

插件默认值是 `true/64/false`，然后依次叠加实例默认与单次扫描请求的部分覆盖。根目录中的文件深度为 0：关闭递归且未给深度时规范为 0；显式 `recursive: false` 搭配非 0 深度拒绝。从关闭切到开启且本层未给深度时恢复 64；开启递归的有效深度只能为 1—64。只覆盖深度会保留继承的递归开关，矛盾时拒绝；null、未知选项、数值字符串均拒绝。

Local Files 当前声明 `schemaRevision=local-import-v2`，`importLyrics=true` 在成功扫描后创建独立歌词补齐任务，只处理该扫描实际观察到的来源及资产版本；全局自动补齐关闭也可显式导入歌词。实例默认开启导入歌词时，配置必须有已启用的本地 Scraper；单次请求遇到 Scraper 不可用会在逐来源结果中明确失败。扫描成功不等于歌词任务成功，结果查询见[补全说明](metadata.md#导入歌词与自动补缺)。音频/NCM/Scraper 实例不接受 `importDefaults`。实例修改须重启；没有动态启停或导入表单界面。

### 自动补齐策略

```yaml
metadata:
  automatic:
    enabled: false
    instancePriority: ['scraper-local']
    fields: ['lyrics', 'artwork']
    languages: []
```

`enabled` 默认false；开启后成功扫描可补齐缺失或不可用且未人工锁定的内容。`fields` 默认歌词和封面，开启时不可为空。`instancePriority` 最多4个不重复的本地Scraper实例，按声明顺序优先；空数组按已启用实例的配置顺序解析；已启用但启动校验失败的实例仍作为失败依赖报告，不静默跳过。全局自动补齐或实例默认导入歌词开启时，显式优先实例必须同时启用，且至少有一个启用的Scraper；没有显式顺序时最多4个启用实例。程序路径校验失败仍按运行时不可用处理，不伪造任务成功。

`languages` 最多16项，空数组不筛选；语言归一、未知语言和封面规则与[手动任务](metadata.md)一致。显式 `importLyrics=true` 使用同一实例优先顺序，但只请求歌词且不继承全局语言过滤。策略在任务准备时持久化，重启后继续已有快照；配置修改重启后用于新准备的任务。自动选用只接受内嵌或同名旁置候选，同优先级存在内容歧义会跳过，目录通用封面不自动认定属于每首歌曲。人工锁始终保留；不会写回原音频或旁置文件。

客户端先读取 Provider 的 `configuration` 与安全 `roots[{id,name}]`，再提交 `mode/schemaRevision/rootIds/options`；不返回磁盘路径或真实根身份。详见[共同契约](contracts/README.md)。扫描任务固定有效选项、根及插件/配置版本，范围外曲目保留；不能把一次浅层扫描当作完整音乐根的删除证明。

### 原件写回

`writeBack` 可省略或部分覆盖，null、未知字段、错误类型和越界值拒绝。根必须引用已配置的 Local Files 实例及其音乐根；关闭写回仍验证引用，开启时要求至少一个明确授权且插件和实例都启用。字段为1—2个不重复的 `lyrics/artwork`，不接受磁盘路径，不默认开放所有根。`Snapshot/Current` 深拷贝根和字段，外部修改返回值不能改变在用授权。

配置不保证存储和工具可写：真实写回仅支持 Windows 固定本地 NTFS 的受支持 FLAC，Provider 逐根报告能力，任务再次复核文件及 ffprobe；Linux/NAS/其他存储明确不支持。修改后重启，没有动态授权管理入口。使用和恢复边界见[写回说明](writeback.md)。

## 4. 配置读写服务

`src/config` 提供以下内部接口，使用同一解析器及SQLite操作日志；尚未接入HTTP管理路由或管理CLI。

| 接口 | 当前职责 |
| --- | --- |
| `OpenStore` | 读取并验证文件，恢复未确认的持久操作；不启动或重新配置插件 |
| `Snapshot` | 返回脱敏有效配置、文件`Revision`、`AppliedRevision`、`EnabledSource=yaml`、可写性、`DiskChanged`与`RestartRequired` |
| `SetEnabled` | 按预期修订与幂等键修改指定插件/实例的`enabled`，保存后返回`saved`；不宣称运行时已改变 |
| `Reload` | 完整验证磁盘文件后更新已加载的期望修订，失败保留原配置；不自动应用到运行进程 |
| `MarkApplied` | 仅供生命周期所有者在确实应用该修订后确认；读取或保存成功不自动调用它 |

修订为原始YAML字节的SHA-256，注释变化也改变修订。写入需要持久日志、匹配的预期修订和8—128字符幂等键；同键同参数返回已有结果，换参数返回冲突。预期修订过期返回`PRECONDITION_FAILED`；磁盘已被外部修改则返回`CONFIG_NOT_APPLIED`，必须先重载或重启，不能把运行旧参数和磁盘新开关拼接执行。无日志、文件/目录只读或不能安全替换时拒绝写入。

修改基于YAML语法树，仅变更选定的开关节点；保留其他字段语义及注释，不承诺原排版逐字节不变，不把有效配置中的默认值或覆盖值整体回写。保存后`Revision`改变，`AppliedRevision`不会自动前进，因此`RestartRequired`保持true，直到生命周期所有者确认应用。当前用户应停服编辑并重启，没有文件监视、自动热重载或动态启停入口。

## 5. 原子替换与恢复边界

写入先将操作意图以`prepared`持久保存到SQLite，记录旧/新修订、幂等键及目标，再在配置同目录创建临时文件、写入并同步、复核磁盘版本后原子替换。成功确认后标记`saved`；若结果不确定，保留待恢复日志，不仅修改内存并谎报完成。

Linux保留文件所有者/组与权限，同步文件及目录后rename；Windows复制原文件DACL并保护其继承，使用带write-through的`MoveFileEx`替换，不退化为跨卷复制。权限或替换失败明确返回错误。配置旁的`.lock`文件协调遵守该锁的进程，锁文件可以保留；它不能防止不遵守锁的外部编辑器在最终检查与替换间竞争。手工编辑应停服或使用同一协调机制。

重启读取到旧修订时将未确认操作标为`notSaved`，读取到预期新修订时标为`saved`，其他修订标为`superseded`；不会重新播放旧操作覆盖新文件。数据库只记录操作与恢复事实，不保存第二份权威启用开关。保存、运行应用和插件退出是不同阶段；后两者的动态管理仍按[插件管理计划](implementation/plugin-management.md)实现。
