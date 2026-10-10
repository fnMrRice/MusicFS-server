# 插件配置数据库与目录管理

Local Files 的目录列表、名称、扫描默认值和监听设置由管理接口维护，持久化到 Server 管理的 `dataRoot/providers/plugin-config.sqlite`。数据库按插件 ID 与实例 ID 隔离；插件不拿数据库文件路径、连接串或任意 SQL 权限。YAML 继续管理部署信息、启用开关和显式目录访问权限；未显式配置权限时使用下述默认授权。

## 部署授权

```yaml
plugins:
  - pluginId: musicfs.localfiles
    instances:
      - instanceId: local
        role: provider
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

先读取 `GET /api/v1/admin/plugin-instances/local/configuration`，取返回的 ETag，然后调用同路径的 PUT：

```json
{
  "musicRoots": [
    {"id": "collection", "name": "我的收藏", "path": "D:\\Music\\Collection"}
  ],
  "importDefaults": {
    "recursive": true,
    "maxDepth": 64,
    "importLyrics": false,
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

保存返回 202 与 operationId，Server 排空旧实例、重建访问授权和监听器，再更新能力；通过插件操作接口读取实际应用结果。持久化成功与实例就绪是不同阶段。Local Files 配置修改不会回写 YAML，即使 YAML 文件只读也可保存；全局插件启用开关及其他插件设置仍走既有 YAML 流程。

配置修订由 YAML 字节、各本地实例数据库配置修订和生效的默认授权快照修订共同产生，仍为 64 位十六进制字符串。客户端应视其为不透明值。默认授权不改变现有配置 API、目录或导入参数字段。数据库事务与持久操作日志支持重试和重启恢复；保存中断后按实际持久化结果恢复，不把旧请求重新覆盖到新设置上。

## 插件数据库接口

当前 Local Files 的插件宿主提供 `host.database.get` 和 `host.database.compareAndSet`，沿用 SDK 的 StateRequest/StateValue：key、revision 与 value，value 是 JSON 文档的字节数组，wire 沿用 Base64 编码。不存在的键返回 revision=0；写入必须匹配当前修订，成功加一。键限 1—80 个小写字母、数字、下划线、连字符，值不超过 512 KiB。调用不能指定插件、实例、数据库路径或 SQL，命名空间来自启动时绑定的实例。

插件可独立读写普通配置键；`host-` 前缀为宿主保留，其中 `host-configuration` 是上述管理设置，只允许插件读取，`host-default-read-directories` 由宿主保存首次默认目录探测结果。插件不能自行写入这些键扩大授权；实际音乐目录、扫描默认值与监听设置的修改必须经过管理接口，防止直接数据库写入绕过校验和生命周期。旧 host.state 接口仍维护曲目身份状态，不在本次更改中迁移。其他角色的宿主尚未接入这两个数据库方法。

数据库存放在已有 providers 备份范围内。迁移部署时应使用停服备份/恢复流程，同时保留 YAML、主数据库、providers 状态与插件配置数据库。

Web 当前仍只有插件状态展示；此次提供完整后端配置接口，目录编辑表单尚未实现。

## 验证记录

2026-10-10，新增默认授权与首次目录探测通过 Windows 单元测试、配置持久化集成测试及 HTTP/插件契约测试。定向端到端验证覆盖默认 Music 授权下的待配置状态、首次不导入、手动选择后扫描、重启保持、清空后不恢复与 YAML 不变；原有目录管理、排空和监听器重建回归通过。配置写回测试因沙箱限制无法复制 Windows ACL，在沙箱外复跑通过。Linux 配置测试包交叉编译通过，尚未在 Linux 上运行。

以下保留此前目录管理功能的验证记录。

2026-10-10，在 Windows 上通过数据库/宿主/目录访问单元测试、配置测试、插件管理集成测试及 HTTP/插件契约测试。端到端验证包括：从空目录配置启动、通过接口添加目录、扫描入库、YAML 字节不变、重启恢复目录与曲目身份，以及原有播放排空和监听器重建。另覆盖实例隔离、并发修订冲突、越界拒绝、授权撤销、只读 YAML 保存、清空目录和写回授权引用数据库根。

实际音频写回端到端测试因未配置 MUSICFS_FFMPEG/MUSICFS_FFPROBE 跳过；此次未将其计为通过。
