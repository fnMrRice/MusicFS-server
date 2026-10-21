# Server YAML 配置

部署配置使用 YAML；Local Files 的目录、名称、扫描默认值和监听设置通过管理接口保存到 Server 数据库。YAML 可显式提供目录授权上限；未配置权限时使用 Server 用户 Music 目录和首次启动探测的默认授权，详见[插件配置数据库与目录管理](../plugins/plugin-configuration.md)。动态启用、配置重建与设置应用已提供管理 API；停用只保存开关，重启 Server 生效；Web 管理员可浏览允许路径并选择音乐子目录，管理 CLI 尚未实现。

启动见[运行说明](running.md)；正式[示例配置](../../configs/server.example.yaml)和[编辑器 Schema](../../configs/server.schema.json)随代码维护。HTTP JSON、JSON-RPC、JSON Schema 和媒体清单继续使用各自格式。

顶层 `storage` 选择内置 SQLite 或受信任的独立持久化插件，顶层 `audioDSP` 配置有序 DSP 链及外部节点；两者为启动配置，修改后重启生效。配置、构建和兼容边界见[存储插件](../plugins/storage-plugins.md)与[DSP 插件](../plugins/dsp-plugins.md)。非空 DSP 链要求启用且成功初始化音频处理器，不可因插件缺失静默直放。内置 DSP 要求音频插件也使用内置执行；外部 FFmpeg 插件与内置 DSP 的组合会被拒绝，避免将内置 gain 放入插件子进程。

网易云、QQ 音乐及 Apple Music 的每个实例可在 `instanceId` 同级配置 `ownerUserId`，省略时兼容为 `admin`。每个 MusicFS 用户使用自己的实例，同一用户的不同平台账号也应配置不同 UUID `instanceId` 并分别登录；单实例只保留一个当前平台账号。非账号插件不接受 `ownerUserId`。实例归属是部署配置，变更需重启；不要通过修改既有实例的归属转移凭据，分配新 UUID 后重新登录。凭据加密范围包含平台、用户及实例，跨用户不可恢复旧凭据。

当前产品登录入口仍只创建 `admin` 会话，尚未实现多用户创建或注册；配置用户归属与服务隔离不代表已交付普通用户登录。`GET /admin/plugins` 及其插件实例列表默认仅展示当前用户的账号实例，保留共享插件实例；管理员显式传入 `allUsers=true` 可查看全部，返回 `ownerUserId` 和可选的已验证 `accountName`。该选项不扩大 `/online-instances`、平台登录/退出及资料请求的用户范围，也不会返回凭据；账号显示名只是展示快照，不用于授权。

## 1. 文件与严格校验

配置路径必须为 `.yaml` / `.yml`，建议复制示例为 `configs/server.local.yaml` 后编辑。只接受一个顶层 mapping 文档，`configVersion` 必须为整数 `1`；大小为1字节至1 MiB，嵌套最多32层。解析器拒绝未知字段、重复键、额外文档、anchor/alias、merge、自定义标签、null及不匹配的类型。

布尔值只接受未加引号的小写 `true` / `false`；`"false"`、`yes`、`on`不替代布尔值。整数字段须为规范十进制，不接受浮点、十六进制、前导零或数值字符串；DSP `params` 单独允许有限数值，拒绝 NaN、Inf 和数值字符串。允许中文、注释、空格及带反斜线的Windows路径；Windows路径建议用单引号，如 `'D:\Music Files'`。语法和字段错误只返回位置与安全说明，不回显原始敏感值。

相对路径以配置文件所在目录为基准，包括默认目录以及环境/命令行覆盖的相对路径。`dataRoot`、`mediaRoot`、`cacheRoot` 必须彼此分离，音乐根不得与任一可写根重叠。数据库仍放本地文件系统；音乐可来自已挂载目录。

## 2. 服务字段与覆盖

| 字段 | 默认值与约束 |
| --- | --- |
| `configVersion` | 必填，当前仅1 |
| `listen` | `127.0.0.1:8080`；合法监听地址和1—65535端口 |
| `dataRoot`、`mediaRoot`、`cacheRoot` | `data`、`media`、`cache`，以配置目录解析，须非空且分离 |
| `webRoot` | 可选构建制品目录；也可通过CLI `--web-root`指定 |
| `logging.directory` | 省略时为生效 `dataRoot` 下的 `logs`；显式相对路径以 YAML 目录解析，不接受空路径 |
| `logging.level` | 默认 `info`；支持小写 `trace/debug/info/warn/error` |
| `logging.plugins` | 默认 `{}`；按插件 ID 覆盖等级，最多128项；未列出的插件继承全局等级 |
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
| `plugins` | 插件配置及显式禁用覆盖；可省略，随包角色自动补齐 |
| `pluginBundle` | 可选全量插件统一程序路径，同时限定同目录的独立在线插件发现位置；省略时检查 Server 程序同目录；空字符串关闭发现 |
| `writeBack.enabled` | 默认false；显式原件写回开关，不由上传/选用/自动补缺开启 |
| `writeBack.allowOverwrite` | 默认false；覆盖旧标签还要求本次请求显式overwrite |
| `writeBack.maxFileBytes` | 默认8589934592（8 GiB），范围46—1099511627776（1 TiB） |
| `writeBack.roots` | 默认空；最多256个不重复的`{instanceId,rootId,fields}`授权，字段限lyrics/artwork |

优先级为内置默认值 → YAML → 支持的环境变量 → 显式CLI参数。支持 `MUSICFS_LISTEN`、`MUSICFS_DATA_ROOT`、`MUSICFS_MEDIA_ROOT`、`MUSICFS_CACHE_ROOT`、`MUSICFS_WEB_ROOT`、`MUSICFS_LOG_LEVEL` 和 `MUSICFS_ADMIN_TOKEN`；当前CLI仅为普通配置提供 `--web-root` 覆盖，`--config`选择文件。插件及实例开关不接受环境或CLI覆盖。

`logging` 可省略或逐项覆盖。日志同时写控制台及文件：服务端 `server.log`，每个插件独立写入 `plugins/<pluginId>.log`；同插件多个实例通过日志字段区分。每天或单文件达到100 MiB轮转，旧日志自动 gzip 压缩，保留30天，运行中每小时清理。日志目录不得与音乐根重叠。配置文件变更在重启后生效；管理员可通过 `GET/PUT /api/v1/admin/logging` 读取或临时调整等级，PUT 须提供完整 `level` 与 `plugins`，并通过登录、Origin 与 CSRF 校验。运行时修改不写回 YAML，重启恢复启动值，具体等级用途及调用示例见[日志](logging.md)。

管理员令牌至少24字符，只通过 `MUSICFS_ADMIN_TOKEN` 注入，不是YAML字段。读写服务的 `Snapshot` 清空该值；凭据、环境变量和CLI生效值不写回文件。不要把凭据放入URL、长期测试材料或Git。

`hashing` 可省略或只覆盖部分字段；其默认值会逐项补齐，显式0、超出上限、未知子字段或类型错误均拒绝。哈希使用独立后台队列和上述共享预算，默认自动关闭，以减少启动和扫描后的磁盘竞争。手动创建任务仍受归属、稳定完整读取能力及资源上限约束；它计算包含容器、标签、内嵌封面的原始文件字节，不能当作解码 PCM 或录音相同性证明。修改这些配置后需重启。

`fingerprinting` 同样允许省略或部分覆盖，显式0、null、未知键或类型错误拒绝。启用时同时要求 `musicfs.plugin.decoder.ffmpeg` 插件和一个音频实例明确启用，复用其 DSP 前规范化能力；不会自动打开音频插件。fpcalc 运行时须校验指定摘要及精确版本 1.6.1，算法固定为 2，不允许请求自选工具路径、算法或版本。禁用时可省略工具路径和摘要，不启动 fpcalc。没有 `auto`、上传、外部识别或远程检索配置项。

指纹功能提供最长120秒的手动片段请求：起点0—86400000毫秒，请求时长10000—120000毫秒。实际读到的尾段可以更短，须如实记录覆盖样本数和 `short` 质量，不能补零冒充完整片段或当作录音匹配依据。工具不可用或摘要/版本不符时只使指纹能力返回 `UNSUPPORTED`，不使整个服务和普通播放退出。创建、取消和按所有者读取明细见[指纹说明](../metadata/identification.md#本地音频指纹)。

## 3. 插件与实例

每个插件包含 `pluginId`、`executable`、可选字符串数组 `args`、可选64位十六进制 `sha256`、`enabled` 和 `instances`。每个实例包含全局唯一 `instanceId`、可选 `name`、`enabled` 与 `config`。实例 ID 使用规范小写、非零 UUID，不由数组位置或显示名生成；音乐根 ID 继续使用1—128个ASCII字母、数字、连字符或下划线。读取旧配置时将旧实例标识稳定转换为 UUID，并同步转换写回授权与自动补齐优先级引用。

实例配置的 `name` 作为旧管理员备注保留，示例配置与随包插件自动发现均不预填。所有插件必须返回显示别名 `plugin.name`，并可通过 `plugin.localizedNames` 提供译名；在线实例使用已验证账号的用户名，未登录或无用户名时回退插件别名。精确语义见[插件身份与显示名称](../plugins/plugin-identity.md)。

角色由插件握手的 `Manifest.roles` 声明，宿主校验其满足对应适配器的角色要求后才允许工作。尚未启动的官方插件使用与 Worker 共用的角色声明展示分类；部署配置不接受 `role`，旧配置升级时需删除实例下的该字段。管理 API 响应中的 `role` 保持不变，客户端无需调整。

| `pluginId` | 角色与实例参数 |
| --- | --- |
| `musicfs.plugin.source.localfiles` | `provider`；`config.musicRoots`包含`id/name/path`。有效启用时至少一个音乐根；根ID在该实例内唯一；可选`config.watch`配置宿主文件通知与周期复核，默认关闭 |
| `musicfs.plugin.decoder.ffmpeg` | `audio`；`config.ffmpegPath`与`config.ffprobePath`。有效启用时两者均必填 |
| `musicfs.plugin.decoder.ncm` | `audio`；`config: {}`，具体功能见[NCM说明](../playback/ncm.md) |
| `musicfs.scraper.local` | `scraper`；`config: {}`，最多16个配置实例，只读取目标Provider授权的内嵌/旁置内容；见[补全任务](../metadata/metadata.md) |
| `musicfs.plugin.source.netease` / `musicfs.plugin.source.qqmusic` / `musicfs.plugin.source.applemusic` | `provider`；`config: {}`，三平台合计最多16实例；业务访问要求已校验平台账号，禁止URL/Cookie/token/账号/本地根配置；能力差异见[在线接口](../providers/online.md) |

当前只接受上述七个官方插件 ID，插件 ID 不可重复。插件通过 `supportsMultipleInstances` 声明是否支持多实例；本地音乐、FFmpeg、NCM 最多配置一个实例，已禁用实例也计数，多个本地目录放在同一实例中。三个在线来源与本地歌词/封面插件保留多实例。插件与实例的 `enabled` 省略时均默认false，只有两层都为true才在启动时启用。禁用不改写兄弟实例或删除已有库。有效启用必须配置可执行路径；禁用对象仍严格检查结构、身份、路径隔离与摘要格式，但不要求其程序或外部工具当前可运行。

官方示例为内置插件和实例显式设置 `enabled: true`；自动发现新建的插件和默认实例也会启用，显式已有项省略开关仍保持禁用。缺少音乐目录或音频工具路径时保留为待配置，不阻止服务启动。设置任意一层 `enabled: false` 可禁用；旧配置中的 false 保持有效。Linux将`executable`改为对应Linux构建路径，程序无`.exe`后缀。可选`sha256`用于钉住管理员审核的程序；省略时采用现有显式本机路径信任与本次启动摘要检查，不代表跨重启钉住版本。

Local Files 的 `permissions.readDirectories` 限定授权上限，`[]` 禁止读取；未声明时使用 Server 用户 Music 目录、旧音乐根兼容授权和首次 Windows 默认目录探测结果。授权目录不会自动加入曲库，实际音乐目录由管理接口选择；详细默认值、探测和越界处理见[部署授权](../plugins/plugin-configuration.md#部署授权)。

### 导入选项

Local Files 实例可在 `config` 同级声明 `importDefaults`，每个 `config.musicRoots[]` 也可设置 `importDefaults` 覆盖该目录的默认值。这些字段的标题、说明、默认值与校验规则由官方插件的 `Manifest.configuration` 提供；宿主加载器共用该声明，实际遍历受宿主的根授权和所选递归范围约束。每个实例最多选择 256 个音乐根，扫描请求只能选择已配置根编号。

文件通知是独立宿主调度配置：`config.watch`默认`enabled:false/debounceMs:1000/maxDelayMs:10000/fullRescanSeconds:3600`；省略字段补默认，显式0/null拒绝。去抖范围100—30000ms，最大等待100—60000ms且不小于去抖；完整复核周期30—86400s。最多8个双层启用的监听实例，每实例256目录。超限或通知不可用退为周期复核，paused实例停止自动提交至重启。它不改变导入默认值或授权根，不是动态启停接口；完整语义及日志见[扫描与文件通知](../library/scanning.md#文件通知与周期补漏)。

```yaml
importDefaults:
  recursive: true
  maxDepth: 0
  importLyrics: false
  skipHidden: true
  respectNoMedia: true
  excludePatterns: ['**/cache', '*.tmp']
```

插件默认递归、深度和歌词导入值为 `true/0/false`，然后依次叠加实例默认、目录默认与单次扫描请求的部分覆盖。根目录中的文件深度为 0：关闭递归且未给深度时规范为 0，只查找当前目录；显式 `recursive: false` 搭配非 0 深度拒绝。从关闭切到开启且本层未给深度时恢复 0。开启递归时，0 表示查找全部子目录、不设深度上限；1—64 表示最多进入的子目录层数，例如 1 包含直接子目录。只覆盖深度会保留继承的递归开关，矛盾时拒绝；null、未知选项、数值字符串均拒绝。

Local Files当前声明 `schemaRevision=local-import-v5`，插件版本0.3.0。高级选项默认 `skipHidden:true/respectNoMedia:true`：跳过点开头文件、目录以及 Windows 隐藏属性条目，遵循 `.nomedia` 标记，跳过其所在目录及子树；不读取标记文件的内容。Web 的「包含隐藏文件和目录」对应 `skipHidden:false`。目录树仍展示授权范围，筛选控制实际导入。关闭某项只取消对应筛选，不扩大访问授权。

`excludePatterns` 默认空数组；实例与每次请求可整组替换，显式 `[]` 清除继承规则，不是追加。规则排序后写入有效计划和幂等摘要，换规则的同键请求冲突；只有本轮完整覆盖的非排除范围可推断缺失，排除范围内既有曲目/资产/证据保持原状。隐藏项和 `.nomedia` 的实际排除范围由宿主记录，也不会把既有曲目标记缺失。它是扫描范围设置，不是撤销读取授权或删除曲库的操作。

每实例/请求最多32条、每条UTF-8最多256字节。规则是根相对路径、用 `/` 分隔且始终大小写敏感：`*` 匹配一层内任意字符，`?` 匹配一个Unicode字符，独立段 `**` 匹配零或多层。`*.tmp` 只匹配根本层，`**/*.tmp` 匹配任意深度，`**/cache` 匹配任意层名为cache的目录及整支内容。不支持绝对/盘符路径、反斜线、空/点路径段、字符类、花括号或否定；重复规则和null拒绝。实际排除/递归/根授权由宿主执行，不能由worker跳过后冒称完整扫描；详细说明与例子见[扫描范围](../library/scanning.md)。

`importLyrics=true` 在成功扫描后创建独立歌词补齐任务，只处理该扫描实际观察到的来源及资产版本；全局自动补齐关闭也可显式导入歌词。实例或目录默认开启时须配置本地 Scraper；依赖暂时禁用不使 YAML 失效，运行任务按来源明确报告不可用。扫描成功不等于歌词完成，见[补全说明](../metadata/metadata.md#导入歌词与自动补缺)。音频/NCM/Scraper 实例不接受 `importDefaults`。本地实例设置可通过管理接口保存到数据库并排空重建，不回写 YAML；Web 在本地音乐标题右侧提供「添加本地目录」，下方列出已添加目录并逐项编辑或删除。查找选项按目录保存，Web 在添加或编辑配置应用成功后自动扫描；直接调用配置 API 不触发扫描。插件管理摘要独立统计允许访问的根目录数量，详见[目录配置](../plugins/plugin-configuration.md)。

### 自动补齐策略

```yaml
metadata:
  automatic:
    enabled: false
    instancePriority: ['d248bf34-1780-59d3-a557-9cf5d889e977']
    fields: ['lyrics', 'artwork']
    languages: []
```

`enabled` 默认false；开启后成功扫描可补齐缺失或不可用且未人工锁定的内容。`fields` 默认歌词和封面，开启时不可为空。`instancePriority` 最多4个不重复的本地Scraper实例，按声明顺序优先；空数组按启用实例的配置顺序解析；已启用但启动校验失败的实例仍作为失败依赖报告。全局自动补齐或实例默认导入歌词开启时，显式优先项必须引用已配置 Scraper，且至少配置一个 Scraper；没有显式顺序时最多4个启用实例。禁用依赖保留策略意图，实际任务明确失败，不伪造补齐成功。

`languages` 最多16项，空数组不筛选；语言归一、未知语言和封面规则与[手动任务](../metadata/metadata.md)一致。显式 `importLyrics=true` 使用同一实例优先顺序，但只请求歌词且不继承全局语言过滤。策略在任务准备时持久化，重启后继续已有快照；配置修改重启后用于新准备的任务。自动选用只接受内嵌或同名旁置候选，同优先级存在内容歧义会跳过，目录通用封面不自动认定属于每首歌曲。人工锁始终保留；不会写回原音频或旁置文件。

客户端先读取 Provider 的 `configuration` 与安全 `roots[{id,name}]`，再提交 `mode/schemaRevision/rootIds/options`；不返回磁盘路径或真实根身份。详见[共同契约](../contracts/README.md)。扫描任务固定有效选项、根及插件/配置版本，范围外曲目保留；不能把一次浅层扫描当作完整音乐根的删除证明。

### 原件写回

`writeBack` 可省略或部分覆盖，null、未知字段、错误类型和越界值拒绝。根必须引用已配置的 Local Files 实例及其音乐根；关闭写回仍验证引用，开启时要求至少一个明确授权。实际写回要求插件、实例和音频探测依赖可用；禁用依赖保留授权配置。字段为1—2个不重复的 `lyrics/artwork`，不接受磁盘路径，不默认开放所有根。`Snapshot/Current` 深拷贝根和字段，外部修改返回值不能改变在用授权。运行中改变根的物理路径不会继承旧写回授权，需重启重新验证路径和身份。

配置不保证存储和工具可写：真实写回仅支持 Windows 固定本地 NTFS 的受支持 FLAC，Provider 逐根报告能力，任务再次复核文件及 ffprobe；Linux/NAS/其他存储明确不支持。修改后重启，没有动态授权管理入口。使用和恢复边界见[写回说明](../metadata/writeback.md)。

## 4. 配置读写服务

管理接口修改音乐目录时，已有根 ID 不能换绑到另一目录；新目录必须使用新 ID。已有根可保留路径并修改显示名。新增根只授予读取／扫描范围，不继承任何旧根写回授权；写回授权仍需显式配置并在重启时验证。

`src/core/config` 提供以下内部接口，使用同一解析器及 SQLite 操作日志；HTTP 动态管理通过 `src/plugins/plugincontrol` 调用，管理 CLI 尚未实现。

| 接口 | 当前职责 |
| --- | --- |
| `OpenStore` | 读取并验证文件，恢复未确认的持久操作；不启动或重新配置插件 |
| `Snapshot` | 返回脱敏有效配置、文件`Revision`、`AppliedRevision`、`EnabledSource=yaml`、可写性、`DiskChanged`与`RestartRequired` |
| `SetEnabled` | 按预期修订与幂等键修改开关或受校验的实例设置，保存后返回`saved`；运行结果由 Manager 单独确认 |
| `Reload` | 完整验证磁盘文件后更新已加载的期望修订，失败保留原配置；不自动应用到运行进程 |
| `MarkApplied` | 仅供生命周期所有者在确实应用该修订后确认；读取或保存成功不自动调用它 |

没有数据库配置或默认授权快照时修订为原始YAML字节的SHA-256；存在本地插件数据库设置或生效的默认授权快照时再纳入相应修订，注释变化也改变修订。客户端须将修订视为不透明值。写入需要持久日志、匹配的预期修订和8—128字符幂等键；同键同参数返回已有结果，换参数返回冲突。预期修订过期返回`PRECONDITION_FAILED`；磁盘已被外部修改则返回`CONFIG_NOT_APPLIED`，必须先重载或重启，不能把运行旧参数和磁盘新开关拼接执行。无日志、文件/目录只读或不能安全替换时拒绝写入。

修改基于 YAML 语法树，仅变更目标开关或设置节点；保留其他字段语义及注释，不承诺原排版逐字节不变，不把有效默认值整体回写。保存后 `Revision` 改变，全部实例实际收敛且没有待重启开关时 Manager 才推进 `AppliedRevision`。管理接口停用只保存开关，当前进程继续使用运行开关，重启 Server 后生效；快照及操作通过 `restartRequired` 提示。启用和实例设置仍可动态应用，设置重建不能提前应用待重启停用。外部编辑配置文件仍须停服并重启，没有自动文件热重载。

## 5. 原子替换与恢复边界

写入先将操作意图以`prepared`持久保存到SQLite，记录旧/新修订、幂等键及目标，再在配置同目录创建临时文件、写入并同步、复核磁盘版本后原子替换。成功确认后标记`saved`；若结果不确定，保留待恢复日志，不仅修改内存并谎报完成。

Linux保留文件所有者/组与权限，同步文件及目录后rename；Windows复制原文件DACL并保护其继承，使用带write-through的`MoveFileEx`替换，不退化为跨卷复制。权限或替换失败明确返回错误。配置旁的`.lock`文件协调遵守该锁的进程，锁文件可以保留；它不能防止不遵守锁的外部编辑器在最终检查与替换间竞争。手工编辑应停服或使用同一协调机制。

重启读取到旧修订时将未确认配置操作标为`notSaved`，读取到预期新修订时标为`saved`，其他修订标为`superseded`；不会重新播放旧操作覆盖新文件。主数据库记录操作与恢复事实，插件配置数据库保存本地实例设置；启用开关仍以 YAML 为准。旧未完成运行操作在重启时标记 superseded，按当前 YAML 启动。保存、运行应用和插件退出是不同阶段，详见[插件管理](../plugins/plugin-management.md)。

## 内置插件程序与自动发现

默认运行只启动 Server `musicfs`，全部官方内置插件都在主进程。普通插件省略 `execution` 时采用 `builtin`；旧路径、参数、摘要可保留，但内置模式不会执行这些文件。安装包继续提供 `musicfs-plugins` 的九种兼容入口，只有显式 `execution: process` 才启动所选程序。SQLite 默认由主进程直接打开，仍是不可禁用的必需组件；官方 DSP gain 同进程运行，只有加入 `audioDSP` 链才参与处理。FFmpeg、ffprobe、fpcalc 工具可按任务外部执行。

内置发现从编译内的固定目录补齐普通插件，不依赖插件文件。省略 `plugins` 时补齐默认目录；显式 `plugins` 列表且未设置 `pluginBundle` 时，只保留列出的范围，`plugins: []` 不隐式开启来源。`pluginBundle` 继续承载兼容的全目录发现/外部入口选择语义，空字符串不再是关闭内置发现的方式。SQLite、DSP 及四类固定算法按各自组装路径管理，不参加普通账号实例发现；不扫描 PATH 或执行任意目录项。

发现范围内自动生成的插件和默认实例使用 `enabled: true`；显式已有项省略 `enabled` 仍保持 false，不借迁移自动启用。任意一层 false 都阻止启动。无需为了禁用自动发现项复制整份配置，例如：

```yaml
plugins:
  - pluginId: musicfs.plugin.decoder.ncm
    enabled: false
```

缺少已选择且可访问的音乐目录或 FFmpeg/ffprobe 工具时，实例保持启用意图，但 `effectiveEnabled=false`、`runtimeState=pending_configuration`、`reason=CONFIGURATION_REQUIRED`，不启动对应执行上下文。外部模式缺失程序也保持待配置；内置模式不因旧插件路径缺失而不可用。设置页显示原因，通过已有实例设置接口选择授权范围内的音乐目录后进入启动流程；默认授权和探测本身不会使实例就绪。权限变化仍需修改 YAML 并重启，当前没有外部程序目录热监控。

内置发现及旧配置迁移只展开内存配置，不因读取而修改 YAML；首次默认音乐目录探测仍将结果保存在插件数据库。管理接口保存开关时保留展开后的身份、执行方式和开关，重启后仍尊重显式禁用。实例 UUID、用户归属、账号状态及目录授权不因执行方式变化而重建或合并。未知插件 ID 仍不允许注册，接口与版本声明不构成第三方自动加载能力。

显式 `execution: process` 保留原文件选择、摘要核验与握手机制：`pluginBundle` 指向全量统一程序；在线独立入口缺失且未固定摘要时可按既有规则选择平台或在线统一入口。固定摘要不自动清除，更新文件须同步审核摘要。统一入口的 `--plugin <用途>` / `--plugin=<用途>` 必须和平台或角色匹配；参数不经 shell，最多 64 项、每项最多 4096 UTF-8 字节，不得含 NUL 或凭据。外部兼容入口仍只有已支持的九种选择值，四类新增算法没有独立进程入口。

内置来源、解码、Scraper 和 DSP 为实例或任务建立独立内存流并保留原协议，在线驱动由受信任装配层编入主进程；SDK 和在线实现仍是独立 Go 模块。内置代码共享地址空间，取消和退出是协作，不提供崩溃隔离或安全强停。只有外部模式回收插件进程。两种模式都继续检查用户、实例、账号和资源授权；升级内置代码需要升级并重启 Server。

发布打包启用 `-ldflags="-s -w"`，开发构建保留调试信息。`release.json` 记录内置实现及兼容外部入口，SQLite 标为必需组件；生成配置默认 `storage.backend: sqlite`。旧官方 SQLite Worker 配置未显式选择 `storage.execution: process` 时映射为内置后端。各插件发行版本独立，依赖与许可证仍按 Server、SDK、在线插件模块共同收集。
