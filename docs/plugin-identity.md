# 音乐来源插件身份与显示名称

在线插件通过 `provider.online.describe` 返回 `plugin` 声明，Local Files 在握手 manifest 中返回同样的声明。Server 校验并缓存插件实际返回的值，再由 `GET /api/v1/online-instances` 或 `GET /api/v1/provider-instances` 传给 Web / Client。内置 Local Files 在 Worker 尚未启动时，复用随 Server 构建的插件自身声明。名称、译名和功能分类归插件所有，宿主和前端不维护平台名称映射。

## 字段与用途

```json
{
  "plugin": {
    "id": "musicfs.netease",
    "name": "NetEase Cloud Music",
    "features": ["music-source"],
    "localizedNames": {
      "zh": "网易云音乐",
      "zh-Hans": "网易云音乐",
      "zh-Hant": "網易雲音樂"
    }
  }
}
```

此示例为插件自己的声明；支持的名称和译名由插件实现提供，更新名称不需要修改 Web。字段定义见 [HTTP Schema](contracts/http.schema.json) 与 [在线插件 Schema](contracts/plugins/v1/online.schema.json)。

| 字段 | 语义 |
| --- | --- |
| `plugin.id` | 插件稳定身份，必须与已验证的握手 manifest 身份一致，不随显示语言改变 |
| `plugin.name` | 插件默认显示名，未找到合适译名时使用 |
| `plugin.localizedNames` | 语言标签到纯文本显示名的映射，可为空对象 |
| `plugin.features` | 插件提供的功能分类；音乐来源声明 `music-source`，与逐项业务能力 `capabilities` 分开 |
| `instanceId` | 具体实例身份，继续用于请求和账号绑定 |
| 实例 `name` | 管理员的实例别名，不能代替插件的显示名或追加到平台标题 |
| 账号 `displayName` | 登录平台后取得的账号昵称，显示在账号状态中 |

默认名和每项译名必须为合法 UTF-8、1—128 个 Unicode 字符，不含控制字符或首尾空白。`localizedNames` 必须为非 null 对象，最多 64 项；语言标签最长 63 字符，使用 `^[A-Za-z]{1,8}(-[A-Za-z0-9]{1,8})*$` 限定形式并由语言标签解析器继续校验，不接受仅大小写不同的重复键。Schema 限定结构与长度；身份一致性、空白、重复语言标签及完整标签合法性由运行时继续检查。

`features` 必须为非 null 数组，最多 32 项、不可重复，每项 1—64 字符并匹配 `^[a-z][a-z0-9]*([.-][a-z0-9]+)*$`。当前在线 Provider 和 Local Files 都必须包含 `music-source`，不因此推断它们支持登录、扫描或列歌单。设置页将来源配置和在线账号操作合并到同一个“音乐来源”区域。

显示名只作为文本渲染，不作为 HTML、链接、请求路径、账号身份或配置值。Web 规范化语言标签后，依次匹配当前标签、同语言的书写体系、主语言、同语言且同书写体系的地区译名，均不匹配时使用插件默认名；例如繁体中文优先 `zh-TW`，再找 `zh-Hant`。插件名称不再送入产品自身的翻译字典；名称改变只影响显示，不改请求中的实例 ID。

## 生命周期与兼容性

- 在线 Worker 修订升级为 `online-authenticated-v3`；其描述必须包含有效的 `plugin`。能力、平台和空配置约束仍单独校验，新增显示字段不扩大插件业务权限。
- Server 使用已验证的 Worker 描述，保留本次 Server 运行期间取得的名称供实例离线或停用时显示；重新成功启动后采用新 Worker 的声明。
- 内置 Local Files 使用插件代码内同一个声明函数提供默认名 `Local Music` 和中文名“本地音乐”。首次待选择目录、禁用、清空选择后重启等尚无 Worker 的状态仍返回 `plugin`；该声明不授予扫描能力，也不启用 Worker。成功启动后以实际 Worker 的已验证声明为准，因此自定义 Worker 的名称不会被内置默认名覆盖。
- 其他从未成功取得声明的实例可省略 HTTP `plugin`。前端显示通用未知来源提示，不根据 `qq-public` 等内部编号或旧配置别名推断名称。运行期间取得的 Worker 声明缓存不持久化到磁盘。
- Server 和在线 Worker 需要同时升级；缺少声明或仍使用 v2 的 Worker 会被拒绝，实例显示为离线。HTTP v1 增加可选字段，现有实例标识、账号绑定及名称字段的含义不变。
- Local Files 的宿主握手同样要求有效声明和 `music-source`；Server 与包含 Local Files 的插件程序须同步重建。通用 manifest 的 `plugin` 字段保持可选，音频和 Scraper 的既有声明无需新增该字段。
- 新 Web 可连接未提供 `plugin` 的旧 Server，但这类在线来源只显示通用提示。旧 Web 可忽略新字段，只有更新 Web 才会采用插件提供的名称与译名。桌面 Client 当前只有设计文档，后续实现须消费该声明。

本次覆盖在线 Provider 和 Local Files 的显示声明，不代表其他角色或未验证第三方插件已获得完整支持；平台登录和业务能力的实际验收范围仍见 [平台账号登录](platform-login.md)。
