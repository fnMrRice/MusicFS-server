# 插件声明式设置

`sdk/pluginsettings` 定义可由 Web、桌面或其他客户端渲染的设置页面。插件提供页面标题、说明、字段和动作声明；客户端不按插件 ID 或字段名称硬编码控件。声明支持 `text`、`number`、`checkbox`、`select` 字段，以及 `actions` 中的按钮。数字输入可声明 `min`、`max`，选择框声明 `options`，所有字段包含 `defaultValue`。

标题、标签和说明可附 `localizedTitles`、`localizedLabels`、`localizedDescriptions`，键为语言标记。未命中翻译时显示插件默认文案。声明中没有脚本、HTML、宿主命令或动作 URL，宿主只路由已注册的动作 ID。

宿主 `src/plugins/settings` 在会话权限范围内提供：

- `GET /api/v1/plugin-settings/{pluginId}`：读取声明、本人持久设置修订、有效值和后台任务状态。
- `PUT /api/v1/plugin-settings/{pluginId}`：提交 `expectedRevision`、`schemaRevision` 和 `values`，验证字段类型、范围、选项和未知字段。成功推进修订，过期写入返回 `CONFLICT`。相同原修订重试不会再次写入；响应不确定时先读取当前设置确认。省略的字段恢复插件默认值，因此客户端保存时应提交完整表单。
- `POST /api/v1/plugin-settings/{pluginId}/actions/{actionId}`：提交空对象，使用 CSRF 与 `Idempotency-Key`。宿主拒绝未声明动作与任意附加请求字段。

插件管理条目的 `settingsAvailable` 表示存在可显示的设置页面。设置按用户和插件 ID 分隔，保存在服务器数据库；其他用户不会读取本人的值或识别任务。当前内置录音匹配插件提供上述页面；进程外插件的自定义设置声明和动作 RPC 尚未实现，不应把此接口描述为允许第三方脚本执行。

自动识别由标准匹配插件提供标题、文件哈希、指纹候选桶与评分策略，也由插件解释自动开关和分数阈值。宿主只提供当前授权证据、持久任务、分批调度、输入输出校验和候选存储。新增设置提供者可注册声明与可选任务/动作处理函数；没有后台任务的普通设置页返回 `idle`。

人工合并、解除和排除始终是 Server 的录音管理能力，独立于匹配插件、自动开关和分数设置。参见[录音聚合曲库](../library/recording-groups.md)。
