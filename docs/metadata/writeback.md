# 显式写回本地 FLAC

显式任务将已选歌词或封面写回原始 FLAC，默认关闭，通过 API 操作。上传、Scraper 搜索、人工选用和自动补缺不会触发写回。配置见[配置说明](../operations/configuration.md)，接口以 [OpenAPI](../contracts/openapi.yaml)为准。

## 授权与存储能力

```yaml
writeBack:
  enabled: false
  allowOverwrite: false
  maxFileBytes: 8589934592
  roots:
    - instanceId: b1548e96-3081-5c9c-8376-bf290e369d51
      rootId: local
      fields: [lyrics, artwork]
```

管理员将 `enabled` 改为 true、为 Local Files 实例/根明确列出字段并重启后才允许提交。根编号来自现有授权，不接受新路径。实例和插件必须同时启用；禁用策略仍检查引用和字段。`roots` 默认空，最多256项；单根字段为不重复的歌词、封面，不提供修改标题、歌手的公共接口。`maxFileBytes` 默认8 GiB，范围1字节至1 TiB，不表示把整文件放入内存；格式结构的最小长度由标签插件验证。

首版仅支持 Windows 固定本地 NTFS 上可安全访问的单链接 FLAC。Linux、网络盘/NAS、非NTFS、符号链接/重解析点、硬链接别名、只读或特殊文件属性均不支持；不会借写回解密 NCM 或转码。启用的音频实例须提供可运行的 ffprobe，用于结果探测；工具缺失返回 `UNSUPPORTED`。

Provider 的 `roots[].writeBack` 返回 `enabled/available/formats/fields/replacement`：开关和实际能力独立，存储可用不自动授权。能力检测只读、不创建探针文件；每次任务仍检查文件格式、身份、大小、权限和属性。`replacement=recoverableReplace` 是有持久日志的可恢复替换，不是单次原子替换；两次受保护重命名之间可能短暂没有原文件名。不支持的存储保留独立媒体对象和人工选择，不尝试更弱的写入方案。

目录 Provider 还须声明 `library.rebind.stable-paths-v1`，使替换后的文件身份能够按共享协议保持原来源编号。未声明时不投影为可写，提交在排队前返回 `UNSUPPORTED`；取得排他租约后再次检查本次 Worker，覆盖排队后的重载和持久任务恢复。恢复遇到能力缺失时保留既有保护屏障，不修改原件或猜测私有状态。

## 内置标签格式插件

FLAC 标签实现位于 `plugins/tags/flac`，通过 `sdk/tagformat.Format` 在 Server 主进程内注入，不启动插件子进程。插件只接收宿主提供的只读源接口和未发布目标写入接口，负责校验标签补丁、解析 FLAC 元数据、重写选中字段和报告音频字节范围；不接收路径、用户账号或文件替换权限。

格式、扩展名和可写字段由 `Format.Descriptor()` 声明，能力投影与提交/执行两层校验使用同一格式契约。宿主路径安全校验不再固定 `.flac`；替换实现必须先声明支持的扩展名，并实际解析和验证音频字节范围。此扩展点不放宽文件系统、来源身份和音频摘要保护，也不表示官方已提供其他格式实现。

宿主 `src/metadata/writeback` 继续负责根目录和字段授权、独占来源、原件保护、持久日志、可恢复替换及数据库发布。宿主对原文件和输出音频分别计算 SHA-512，校验插件返回的范围与摘要；输出改变音频时在重命名前拒绝。恢复任务仍复核已记录的文件身份、完整摘要和音频摘要，不因插件化放宽原件保护。

当前内置实现只覆盖原有 FLAC 范围，保留未选择的标签、未知元数据块和音频字节；未增加 ID3、MP4、歌词对齐或其他文件格式支持。公共写回 API 与授权配置保持原有范围。

## 创建与查询任务

先通过[内容接口](media.md)上传或从[补全候选](metadata.md)选用内容，再 GET `/api/v1/source-tracks/{id}/media-selection` 取得当前资产和选择修订。请求示例：

```json
{
  "sourceTrackId": "来源编号",
  "assetId": "当前资产编号",
  "assetVersion": "当前资产版本",
  "expectedRevision": 2,
  "fields": [{"name": "lyrics", "language": "und"}],
  "overwrite": false
}
```

POST `/api/v1/writeback-jobs` 携带会话、允许的 Origin、CSRF 和 `Idempotency-Key`，返回202 `{jobId,merged}`。六个顶层字段均须提供。每字段最多一项，只能选当前有效、可用的绑定；首版写一个指定语言的歌词及一张封面。歌词语言空或 `und` 统一为 `und`，封面 language 必须空。不能传路径、原音频正文、图片字节或替代对象路径。

默认补原文件缺少的标签，不删除旧标签或未选择字段。覆盖旧标签同时要求配置 `allowOverwrite: true` 和请求 `overwrite: true`。多个歌词语言必须明确选一项；同键同参数重放原任务，源已更新后仍可读取原结果，改参数须新键。过期资产、选择修订变化、缺失或不可用对象明确拒绝。

GET `/api/v1/writeback-jobs/{id}` 返回所有者范围任务，状态为 `queued/running/recovery/succeeded/failed/cancelled`。活动播放、扫描或稳定读取期间保持排队，结束占用后才取得独占权。成功结果含旧/新资产、新版本和完整原文件 SHA-512；来源编号保持稳定，全部当前可用媒体选择及人工锁迁到新资产，旧资产证据不冒充新文件证据。原音频帧不转码。

POST `/api/v1/writeback-jobs/{id}/cancel`，正文 `{}`，只取消 queued；重复 cancelled 返回同一结果，running/recovery 不做隐式回滚。操作自然幂等，无需额外幂等键，仍要求 Origin/CSRF。

## 恢复与备份

替换前在原文件所在文件系统准备临时文件和备份，持久记录旧/新身份和阶段。启动先处理日志，再开放读取来源的后台任务；不把同名文件存在当作完成。不能确定的外部改动或不可用资源进入 recovery，保留来源屏障，不自动覆盖未知内容。

POST `/api/v1/writeback-jobs/{id}/recover`，正文 `{}`，检查当前授权后安排原任务在后台重试恢复，200只表示返回当前任务，须继续 GET 轮询最终状态。没有新任务ID；重复调用不重复排队。已完整完成的 succeeded 返回原结果，其余非 recovery 状态冲突。数据库已提交但文件日志尚未完成时，公开状态仍为 recovery。禁用策略可读取历史，不允许借恢复继续写入。原件未变且尚未替换时可终止为 failed，此时须检查当前状态后新建任务。

正常停服后保留整个 `dataRoot` 的数据库、Local Files 身份映射和写回日志，以及音乐根内的恢复文件；不要删除未解决任务的备份/临时文件来解除屏障。媒体对象仍按[内容备份规则](media.md#发布回收与备份)备份。当前没有自动清理成功写回备份的管理入口。

## 验证

2026-10-21 已通过 `tests/integration/writeback` 完整测试及 `tests/integration/writebackhost` 测试，覆盖未选择标签与音频保留、显式覆盖、取消、权限、备份和进程中断恢复。新增注入测试验证未提供格式插件时拒绝构造，以及插件输出篡改音频时保留原件、不提交资产。NTFS 替换、硬链接和 ACL 用例在沙盒外复核通过；沙盒限制下的文件操作失败不作为产品行为结论。
