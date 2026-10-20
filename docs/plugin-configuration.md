# 插件配置数据库与目录管理

Local Files 的目录列表、名称、扫描默认值和监听设置由管理接口维护，持久化到 Server 管理的 `dataRoot/providers/plugin-config.sqlite`。数据库按插件 ID 与实例 ID 隔离；插件不拿数据库文件路径、连接串或任意 SQL 权限。YAML 继续管理部署信息、启用开关和显式目录访问权限；未显式配置权限时使用下述默认授权。

## 部署授权

```yaml
plugins:
  - pluginId: musicfs.plugin.source.localfiles
    instances:
      - instanceId: b1548e96-3081-5c9c-8376-bf290e369d51
        permissions:
          readDirectories: ['D:\Music', 'Z:\SharedMusic']
        config: {}
```

该片段用于已发现随包插件的配置。没有选择音乐目录时，实例处于 `pending_configuration`，Server 仍可启动。授权目录本身不会自动加入曲库；实际音乐目录由接口选择，可以是授权目录或其子目录。相对授权路径以 YAML 所在目录为基准；接口新增目录必须提交 Server 上的绝对路径。

未提供 `permissions` 的 Local Files 实例默认允许当前 **Server 运行用户**的 Music 目录：Windows 为 `%USERPROFILE%/Music`，其他平台为 `~/Music`；这不是远程 Client 用户的目录。原 YAML `musicRoots` 路径继续提供兼容授权。显式 `permissions.readDirectories` 完整替换这些默认授权及下面的探测结果，不自动合并；`permissions.readDirectories: []` 明确禁止文件访问。接口不能修改 permissions，也不能修改插件程序路径。

Windows 上，使用默认权限的实例首次启用并启动 Server 时，宿主检查每个逻辑磁盘根目录下的固定目录名：`Music`、`NeteaseMusic`、`CloudMusic`、`QQMusic`、`Kugou`、`KuwoMusic`，例如 `D:\Music` 与 `E:\NeteaseMusic`。仅将存在且可读的目录加入允许范围；与 `dataRoot`、`mediaRoot` 或 `cacheRoot` 重叠的探测路径排除。该过程只检查这些候选目录，不递归搜索整盘，不读取音乐内容，也不自动启动扫描。

探测结果按实例持久化到 `plugin-config.sqlite` 的宿主专用键 `host-default-read-directories`，没有找到目录也保存空结果。后续读取配置和重启使用已有结果，不重复探测；其他平台不探测磁盘根目录。默认授权及探测结果不写回 YAML。新增默认允许目录不会添加或恢复 `musicRoots`，也不会导入音乐；用户仍须通过管理接口选择实际音乐目录，再发起导入。

旧 `musicRoots/importDefaults/watch` 作为数据库尚未保存时的初始值，首次接口保存写入完整设置快照；后续以数据库为准，包括主动清空的目录列表。未选择实际音乐目录时，即使存在默认授权，实例仍处于 `pending_configuration`。

调整 YAML 授权需重启。若授权缩小，数据库设置保留，但越界实例进入待配置且不启动；管理员可通过接口移除越界目录。宿主在设置校验和启动阶段检查路径，在实际读取时从授权目录句柄相对打开所选目录，拒绝通过符号链接或 junction 逃逸。该约束覆盖宿主提供的文件访问接口，不是对插件进程自身 OS 权限的沙箱承诺。

## 管理接口

管理员先调用 `GET /api/v1/admin/plugin-instances/{instanceId}/directories` 查看允许访问的根目录。省略 `path` 时响应的 `path`、`parentPath` 均为 `null`；每项包含 `name`、Server 绝对 `path` 与 `available`。不存在、离线或不可读的授权根仍显示为 `available: false`，不能选择；不会自动创建目录。没有选定音乐目录、未启动 Worker 或实例处于 `pending_configuration`，均不影响这一管理入口。

将选中项的路径作为 URL 编码的 `path` 查询参数请求同一接口，只返回该目录的直接子目录，不返回文件、曲目或文件内容。可以选允许根本身，也可以逐层进入并选择子目录；`parentPath` 最多回到所属授权根，在根本身为 `null`。`available` 只代表当前可读取，保存仍复核目录权限及与数据、媒体、缓存根的隔离。配置 GET 和普通 Provider 接口继续脱敏；只有此管理员浏览接口公开磁盘路径。

响应和每个目录项还包含 `configuredRootId`：已选择的目录返回其音乐根 ID，否则为 `null`；根列表页的顶层值为 `null`。客户端据此保留目录身份、避免重复添加，编辑中先移除再选回时可复用原 ID 并省略 path。Server 按规范路径、符号链接目标及 Windows 不区分大小写规则匹配；保存同一目录到多个 ID 会被拒绝。

查询仅接受可选 `path` 与 `cursor`，空值、重复字段、未知字段及无效编码拒绝。路径必须为 Server 的绝对路径，拒绝 `.`、`..`、控制字符及 Windows 设备路径、扩展命名空间、ADS、保留设备名与末尾点/空格；明确授权的标准 UNC 共享路径可用。实际打开从授权根句柄相对进行，符号链接或 junction 不能逃逸。越界路径为 `ACCESS_DENIED`，目录不可读取或链接逃逸为 `SOURCE_UNAVAILABLE`，不回显系统错误或目标路径。

每页最多检查 100 个原始条目，只返回其中的目录，因此 `items: []` 仍可能带 `nextCursor`；继续传相同 `path` 和游标，直到 `nextCursor: null`。游标绑定实例、路径、配置修订及目录修改时间，过期为 `CURSOR_EXPIRED`，客户端重新打开当前目录；目录列表使用文件系统顺序而非排序快照。为限制一次请求的回放工作，游标偏移最多 100 万条；超过此限需缩小部署允许目录范围。响应带配置 ETag 和 `Cache-Control: no-store`。

先读取 `GET /api/v1/admin/plugin-instances/{instanceId}/configuration`，取返回的 ETag，然后调用同路径的 PUT：

```json
{
  "musicRoots": [
    {"id": "collection", "name": "我的收藏", "path": "D:\\Music\\Collection"}
  ],
  "importDefaults": {
    "recursive": true,
    "maxDepth": 0,
    "importLyrics": false,
    "skipHidden": true,
    "respectNoMedia": true,
    "excludePatterns": []
  },
  "watch": {
    "enabled": true,
    "debounceMs": 1000,
    "maxDelayMs": 10000,
    "fullRescanSeconds": 3600
  }
}
```

使用管理员会话、CSRF、`If-Match` 与 `Idempotency-Key`。GET 继续返回脱敏配置与可写字段 Schema，不公开真实磁盘路径；普通 Provider 接口也只公开根 ID 和名称。设置字段省略时保留，musicRoots/watch/importDefaults 提交时整组替换。已有根可以省略 path 改名；同一根 ID 不可换绑目录。新增目录须存在且可访问。空 musicRoots 表示移除全部目录，实例恢复待配置，不删除已有曲库记录。

保存返回 202 与 operationId，Server 排空旧实例、重建访问授权和监听器，再更新能力；通过插件操作接口读取实际应用结果。持久化成功与实例就绪是不同阶段。Local Files 配置修改不会回写 YAML，即使 YAML 文件只读也可保存；全局插件开关及其他插件设置仍走既有 YAML 流程；停用只保存开关，重启 Server 后生效。配置重建使用当前运行开关，不提前应用待重启停用。

配置修订由 YAML 字节、各本地实例数据库配置修订和生效的默认授权快照修订共同产生，仍为 64 位十六进制字符串。客户端应视其为不透明值。默认授权不改变现有配置 API、目录或导入参数字段。数据库事务与持久操作日志支持重试和重启恢复；保存中断后按实际持久化结果恢复，不把旧请求重新覆盖到新设置上。

## 插件数据库接口

当前 Local Files 的插件宿主提供 `host.database.get` 和 `host.database.compareAndSet`，沿用 SDK 的 StateRequest/StateValue：key、revision 与 value，value 是 JSON 文档的字节数组，wire 沿用 Base64 编码。不存在的键返回 revision=0；写入必须匹配当前修订，成功加一。键限 1—80 个小写字母、数字、下划线、连字符，值不超过 512 KiB。调用不能指定插件、实例、数据库路径或 SQL，命名空间来自启动时绑定的实例。

插件可独立读写普通配置键；`host-` 前缀为宿主保留，其中 `host-configuration` 是上述管理设置，只允许插件读取，`host-default-read-directories` 由宿主保存首次默认目录探测结果。插件不能自行写入这些键扩大授权；实际音乐目录、扫描默认值与监听设置的修改必须经过管理接口，防止直接数据库写入绕过校验和生命周期。旧 host.state 接口仍维护曲目身份状态，不在本次更改中迁移。其他角色的宿主尚未接入这两个数据库方法。

数据库存放在已有 providers 备份范围内。迁移部署时应使用停服备份/恢复流程，同时保留 YAML、主数据库、providers 状态与插件配置数据库。

Web 管理员从「本地音乐」标题右侧的「管理目录」直接打开一个弹窗，目录树以多个 Server 允许访问的目录为根，可逐层展开并选择根或子目录；允许路径只读，不能在界面扩大授权。首次没有已选音乐目录，标题下显示「暂无已选择目录」；已选择的目录在标题下列出。

弹窗下方提供「递归查找子目录」复选框，重新启用递归默认深度为 0。深度和筛选位于默认折叠的「高级选项」：「限制递归深度」未勾选时保存为 0，勾选后数字才可编辑，1—64 限制最多进入的子目录层数，填写 0 仍表示全部递归。关闭递归只查当前目录，并保存 `recursive:false/maxDepth:0`。

高级选项默认不勾选「包含隐藏文件和目录」，对应 `skipHidden:true`，跳过点开头及 Windows 隐藏属性的文件、目录；默认勾选「遵循 .nomedia 标记」，对应 `respectNoMedia:true`，跳过标记所在目录及子树。这些规则用于实际扫描，不修改允许路径。控件只调整本次弹窗草稿，取消不改变已保存设置。保存时目录与完整导入选项一同写入实例数据库，保留原有歌词与排除规则，不自动扫描。配置 GET 始终明确返回两个高级选项的布尔值，包括 `false`。

## 验证记录

2026-10-10，来源名称与弹窗调整通过 Web 构建、68 项单元测试及 7 项浏览器回归。真实 Server 验证初次待配置与清空重启均显示「本地音乐」、零默认已选目录、授权目录列表只读、弹窗取消/Esc 无副作用及焦点返回、确认添加后再保存、保存不自动扫描和重复选择保护；Mock 回归包含分页、游标失效、普通账号隔离及 390px 宽度布局。Server 定向测试覆盖未启动 Worker 的名称声明、已验证 Worker 名称优先及生成 Windows/Linux 包配置的默认授权与零导入目录；未运行实际安装包音频端到端测试。

2026-10-10，Web 构建与类型检查通过，目录管理相关浏览器回归 6/6 通过。其中两项使用真实 Server、Local Files Worker 与 Edge，覆盖授权根进入中文子目录、保存不自动扫描、显式扫描只导入所选子目录、重复选择保护、移除后重选保留原目录身份，以及清空后重启仍待配置且曲库记录保留；三项 Mock 测试覆盖空页继续分页、游标失效后从当前目录首页重试和普通账号无管理入口，另有插件状态入口回归。Web 单元测试 61/61 通过。Linux Server 交叉构建通过，未计为 Linux 运行验证。

2026-10-10，管理员目录浏览通过 Windows 配置与 HTTP 集成测试，覆盖空目录配置、管理员隔离、失效授权根、仅返回目录、分页及游标失效、非法路径、UNC 路径语法、符号链接和 junction 越界、句柄打开后的目录替换，以及选中子目录持久化与重复路径拒绝。目录响应模型及 OpenAPI 引用通过契约验证；配置读取仍不公开路径。

2026-10-10，新增默认授权与首次目录探测通过 Windows 单元测试、配置持久化集成测试及 HTTP/插件契约测试。定向端到端验证覆盖默认 Music 授权下的待配置状态、首次不导入、手动选择后扫描、重启保持、清空后不恢复与 YAML 不变；原有目录管理、排空和监听器重建回归通过。配置写回测试因沙箱限制无法复制 Windows ACL，在沙箱外复跑通过。Linux 配置测试包交叉编译通过，尚未在 Linux 上运行。

以下保留此前目录管理功能的验证记录。

2026-10-10，在 Windows 上通过数据库/宿主/目录访问单元测试、配置测试、插件管理集成测试及 HTTP/插件契约测试。端到端验证包括：从空目录配置启动、通过接口添加目录、扫描入库、YAML 字节不变、重启恢复目录与曲目身份，以及原有播放排空和监听器重建。另覆盖实例隔离、并发修订冲突、越界拒绝、授权撤销、只读 YAML 保存、清空目录和写回授权引用数据库根。

实际音频写回端到端测试因未配置 MUSICFS_FFMPEG/MUSICFS_FFPROBE 跳过；此次未将其计为通过。
