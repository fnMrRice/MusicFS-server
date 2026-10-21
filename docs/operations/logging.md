# 服务端与插件日志

日志使用 Go 标准库 `log/slog` 记录结构化事件，文件轮转由 `lumberjack` 管理。默认最低等级为 `info`，控制台与文件使用同一套等级过滤。服务端日志写入 `logging.directory/server.log`，插件日志写入 `logging.directory/plugins/<pluginId>.log`；同一插件的多个实例共用文件，通过 `instanceId` 区分。

省略 `logging.directory` 时使用生效 `dataRoot` 下的 `logs`。例如 `dataRoot: '../data'` 对应 `../data/logs`；显式日志相对目录与其他路径一样，以 YAML 文件所在目录为基准。日志目录、日志内容不通过等级管理 API 暴露。

日志每天按 UTC 日期或达到 100 MiB 时轮转，历史文件自动 gzip 压缩，保留 30 天。启动和轮转时处理归档，运行中每小时清理过期文件；停机期间的旧文件按原修改时间计算保留期限，已移除插件的遗留日志也会归档清理。压缩由宿主维护线程统一处理，完整 gzip 同步落盘后才移除原文；服务真实清理完成后关闭文件并等待剩余归档，再释放日志目录锁。运行日志用于排错，业务任务结果仍以任务 API 与数据库状态为准。

同一日志目录只能由一个服务进程持有；嵌入调用安装全局日志时，同一进程只允许一个托管服务，避免不同服务的后台日志混写。停机超时但后台清理尚未结束时，日志与目录锁继续保留到真实清理完成。

## 等级与启动配置

| 等级 | 主要用途 |
| --- | --- |
| `trace` | 高频诊断步骤、协议与逐项处理摘要，定位具体操作过程 |
| `debug` | 请求耗时、任务调度、插件调用和状态判断的诊断信息 |
| `info` | 启动停止、配置变更、任务完成或正常取消及主要业务状态变化 |
| `warn` | 可恢复失败、任务暂停、依赖不可用及重试等需要关注的事件 |
| `error` | 扫描终止失败、状态无法持久化、需恢复的存储或服务异常 |

等级是最低门槛，例如 `debug` 同时保留 `debug/info/warn/error`。配置示例：

```yaml
logging:
  directory: '../data/logs'
  level: info
  plugins:
    musicfs.plugin.source.localfiles: debug
    musicfs.plugin.decoder.ffmpeg: trace
```

`MUSICFS_LOG_LEVEL` 可覆盖 YAML 的全局等级，插件显式覆盖仍优先。等级仅接受表中的小写名称；null、空字符串、未知字段和非法插件标识均拒绝。文件配置变化需重启；不会因插件管理操作自动重载日志设置。

日志记录事件、内部身份、数量、阶段、耗时及安全错误类别。诊断级别不应写入管理员令牌、Cookie、账号凭据、完整请求体、歌曲内容或上游响应正文。

外部进程插件的标准输出继续专用于协议；标准错误中的 SDK 结构化诊断按宿主绑定的插件身份、等级及安全字段归档。未知原始文本仅记录字节数和脱敏摘要，避免第三方程序意外输出凭据或私有路径。

## 运行时管理接口

`GET /api/v1/admin/logging` 返回当前全局等级和插件覆盖：

```json
{"level":"info","plugins":{}}
```

`PUT /api/v1/admin/logging` 使用相同 JSON 结构，要求管理员会话、正确的 `Origin` 和 `X-CSRF-Token`。例如临时追踪本地扫描而保持其他模块为 `info`：

```json
{"level":"info","plugins":{"musicfs.plugin.source.localfiles":"trace"}}
```

`level` 与 `plugins` 均必填，PUT 原子替换全部覆盖；`{"level":"info","plugins":{}}` 恢复全局 `info` 并清空插件覆盖。非法请求返回400且原配置保持不变；未登录返回401，非管理员或 CSRF/Origin 不符返回403。修改立即作用于后续日志，不重新启动插件。

接口只修改当前进程内的等级，不保存到 YAML 或数据库；重启后按 YAML 与环境变量恢复。需要长期保留时同步编辑配置文件。此接口只调整日志等级，文件保存期限、压缩策略与路径不接受运行时修改。HTTP 契约见 [OpenAPI](../contracts/openapi.yaml)。
