# 插件身份、显示别名与实例

采用通用控制协议的插件必须在握手 manifest 中提供 `plugin` 身份声明、`supportsMultipleInstances` 布尔声明和有效的 `pluginVersion` 发行版本；其中 `plugin.name` 是必填显示别名，不能用插件 ID 或实例 UUID 代替。在线插件的 `provider.online.describe` 返回同一身份，宿主校验它与握手一致。Server 通过 `GET /api/v1/admin/plugins` 的插件项返回身份、多实例能力与必填 `version`，通过来源接口返回来源身份。尚未启动的官方插件复用与 Worker 同源的声明；已验证的运行声明优先。别名、译名和功能分类归插件所有，前端不维护 ID 到名称的固定映射。DSP 与持久化插件使用各自专用握手，同样必须声明发行版本，见 [DSP 插件](dsp-plugins.md)与[存储插件](storage-plugins.md)。

## 命名与多实例规则

| 插件 ID | 中文显示别名 | 支持多实例 |
| --- | --- | --- |
| `musicfs.plugin.source.localfiles` | 本地音乐 | 否 |
| `musicfs.plugin.source.netease` | 网易云音乐 | 是 |
| `musicfs.plugin.source.qqmusic` | QQ 音乐 | 是 |
| `musicfs.plugin.source.applemusic` | Apple Music | 是 |
| `musicfs.plugin.decoder.ffmpeg` | FFmpeg 音频编解码 | 否 |
| `musicfs.plugin.decoder.ncm` | NCM 解密 | 否 |
| `musicfs.scraper.local` | 本地元数据提取 | 是 |

来源与解码插件分别采用 `musicfs.plugin.source.xxx` 和 `musicfs.plugin.decoder.xxx`；歌词/封面插件 ID 保持原样。多实例能力不从 ID 前缀推断。宿主在启动前使用官方插件共享声明检查配置，启动时再校验实际握手；不支持多实例的插件最多有一个配置实例，已禁用的实例也计数。本地音乐的多个目录配置在同一个实例中。

## 字段与用途

```json
{
  "plugin": {
    "id": "musicfs.plugin.source.netease",
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

此示例为插件自己的声明；支持的名称和译名由插件实现提供，更新名称不需要修改 Web。字段定义见 [HTTP Schema](../contracts/http.schema.json) 与 [在线插件 Schema](../contracts/plugins/v1/online.schema.json)。

| 字段 | 语义 |
| --- | --- |
| `plugin.id` | 插件稳定身份，必须与已验证的握手 manifest 身份一致，不随显示语言改变 |
| `plugin.name` | 插件必须提供的默认显示别名，未找到合适译名时使用 |
| `plugin.localizedNames` | 语言标签到纯文本显示名的映射，可为空对象 |
| `plugin.features` | 插件提供的功能分类；音乐来源声明 `music-source`，与逐项业务能力 `capabilities` 分开 |
| `supportsMultipleInstances` | 插件是否允许多份独立配置；通过握手及管理接口查询 |
| manifest `pluginVersion` / 管理项 `version` | 必填插件发行版本，使用最多 128 字符的有效 SemVer；不是 Server 版本、协议版本或实例修订 |
| 管理项 `displayCount` | 可选 `{value, unit, localizedUnits}`；本地音乐由宿主统计授权根，其他来源由插件提供，只汇总当前管理范围内的完整有效值 |
| `instanceId` | 全局唯一、稳定的小写 UUID，用于请求、配置和账号绑定；不随用户名改变 |
| 在线实例 `name` | 当前已校验平台账号的用户名；未登录或无用户名时使用插件显示别名 |
| 账号 `displayName` | 登录平台后取得的账号昵称，显示在账号状态中 |

部署配置中的实例 `name` 作为旧管理员备注保留，示例与随包插件自动发现不预填，也不以 UUID 填充。在线实例显示名称由平台账号提供，不能用配置备注冒充用户名；同平台多实例可显示“插件别名 · 用户名”，未登录仅显示插件别名。账号退出或切换后及时清理旧用户名。账号名可能重复，操作仍绑定 UUID。

随包发行版本来自 [sdk/bundledplugin/releases.go](../../sdk/bundledplugin/releases.go)，与 Worker 共用同一声明。尚无有效握手版本时，管理项仍返回该声明，包括没有实例、未启动、待配置、已禁用或启动失败的情况；成功握手提供有效版本后优先采用实际版本。版本缺失、空白或不符合 SemVer 的握手被拒绝；版本字段存在不表示插件可用，运行状态仍独立报告。未知插件 ID 继续受现有配置策略限制，不伪造发行版本。

默认名和每项译名必须为合法 UTF-8、1—128 个 Unicode 字符，不含控制字符或首尾空白。`localizedNames` 必须为非 null 对象，最多 64 项；语言标签最长 63 字符，使用 `^[A-Za-z]{1,8}(-[A-Za-z0-9]{1,8})*$` 限定形式并由语言标签解析器继续校验，不接受仅大小写不同的重复键。Schema 限定结构与长度；身份一致性、空白、重复语言标签及完整标签合法性由运行时继续检查。

`features` 必须为非 null 数组，最多 32 项、不可重复，每项 1—64 字符并匹配 `^[a-z][a-z0-9]*([.-][a-z0-9]+)*$`。当前在线 Provider 和 Local Files 都必须包含 `music-source`，不因此推断它们支持登录、扫描或列歌单。设置页将来源配置和在线账号操作合并到同一个“音乐来源”区域。

NCM 与 FFmpeg 声明 `audio-codec`，在插件列表归入“音频编解码”；声道转换与其他 DSP 声明 `audio-dsp`，归入“DSP”。分类由声明决定，不根据插件 ID 前缀推断。声道转换继续保留稳定 ID `musicfs.plugin.decoder.channelmix`，已有配置、日志关联和处理身份不因源码归类改变；其实现位于 `plugins/dsp/channelmix`。Web 继续识别旧版本插件声明的 `audio-processing`、`audio-decryption` 与 `decode` 分类。

显示名只作为文本渲染，不作为 HTML、链接、请求路径、账号身份或配置值。Web 规范化语言标签后，依次匹配当前标签、同语言的书写体系、主语言、同语言且同书写体系的地区译名，均不匹配时使用插件默认名；例如繁体中文优先 `zh-TW`，再找 `zh-Hant`。插件名称不再送入产品自身的翻译字典；名称改变只影响显示，不改请求中的实例 ID。

## 插件管理的展示统计

Web 插件列表通过管理接口的 `displayCount` 展示有明确语义的统计。本地音乐由宿主统计有效部署权限中的根目录，并提供“个允许访问的目录”单位；该数量不包含子目录，也不按已添加的 `musicRoots` 计算。授权属于宿主，未添加路径不传给 Worker；待配置、禁用或 Worker 离线时仍可展示授权数量。需要平台账号的在线来源继续自行提供“用户”统计，Server 不根据实例列表长度、`ownerUserId` 或功能推算。Web 只显示收到的数字和单位，语言选择复用插件名称规则；例如 `{value: 2, unit: "allowed directories", localizedUnits: {"zh": "个允许访问的目录"}}` 显示为“2 个允许访问的目录”。

统计只能为 0—9007199254740991 的整数，单位与译名沿用名称的长度、纯文本和语言标签限制。管理层先按当前用户或管理员显式 `allUsers=true` 筛选，再汇总可见范围内的宿主统计或 Worker 缓存；多个可见配置只有默认单位与全部译名完全一致时才相加。任何可见配置缺少统计、数据非法或总数超出安全整数范围时省略整个字段，避免显示部分总数；没有可见配置时也省略。依赖 Worker 的统计在 Worker 不可用时缺失，本地授权统计不依赖 Worker。明确报告的 0 是有效值，缺失不能转成 0。内部配置实例仍用于权限和运行管理，此展示统计不改变底层多配置协议。

管理查询不等待插件 RPC。本地授权数量直接读取当前生效的权限配置；历史 `plugin.displayCount` 已选根统计不用于此管理摘要。在线来源随账号结果刷新，在退出、过期或 Worker 结束时按插件结果与缓存生命周期更新或清除。未提供统计时，Web 隐藏统计。插件与 Host 使用严格解码，新增 RPC 和账号结果字段须配套升级。具体约束见[控制协议](../contracts/plugins/v1/README.md)与[账号扩展](../contracts/plugins/v1/account.md)。

## 生命周期与兼容性

- 在线 Worker 使用修订 `online-authenticated-v3`；其描述必须包含有效的 `plugin`。能力、平台和空配置约束仍单独校验，新增显示字段不扩大插件业务权限。
- Server 使用已验证的 Worker 描述，保留本次 Server 运行期间取得的名称供实例离线或停用时显示；重新成功启动后采用新 Worker 的声明。
- 内置 Local Files 使用插件代码内同一个声明函数提供默认名 `Local Music` 和中文名“本地音乐”。首次待选择目录、禁用、清空选择后重启等尚无 Worker 的状态仍返回 `plugin`；该声明不授予扫描能力，也不启用 Worker。成功启动后以实际 Worker 的已验证声明为准，因此自定义 Worker 的名称不会被内置默认名覆盖。
- 其他从未成功取得声明的实例可省略 HTTP `plugin`。前端显示通用未知来源提示，不根据 `qq-public` 等内部编号或旧配置别名推断名称。运行期间取得的 Worker 声明缓存不持久化到磁盘。
- Server、随包 Worker 和 Web 必须配套升级；旧插件 ID、缺少必填身份或多实例声明不匹配的 Worker 会被拒绝。管理接口增加插件身份和多实例能力，实例字段改用 UUID；旧客户端的硬编码名称、实例 ID 或严格响应校验需要同步更新。
- 通用 manifest 的 `plugin` 字段必须存在，音频与 Scraper 同样提供别名。音乐来源还必须声明 `music-source` 功能。
- 新 Web 可连接未提供 `plugin` 的旧 Server，但这类在线来源只显示通用提示。旧 Web 可忽略新字段，只有更新 Web 才会采用插件提供的名称与译名。桌面 Client 当前只有设计文档，后续实现须消费该声明。

这些声明不代表未验证第三方插件已获得完整支持；平台登录和业务能力的实际验收范围仍见 [平台账号登录](../providers/platform-login.md)。

## 旧配置与已有数据

- 加载旧 YAML 时先规范化官方插件 ID，再自动发现随包插件，避免重复创建。原始文件字节继续用于配置修订检查；管理员后续保存时写出新 ID。
- 旧实例名使用固定命名空间生成稳定 UUID v5；已有 UUID 保持身份，重复读取和重启不会创建新实例。新的独立实例应生成自己的 UUID，不复用插件 ID、用户名或另一个实例 UUID。写回目录授权和自动刮削优先级同步转换实例引用。
- 插件配置数据库在事务中迁移作用域，保留值与修订；主库同步迁移来源引用，保留曲目、资产、专辑、歌单及条目身份。旧、新作用域发生碰撞时拒绝迁移，不自动合并。
- 本地状态和账号目录可以继续沿用旧目录名，只在旧名与目标 UUID 严格对应时读取。账号迁移先用原平台、用户、实例作用域验证并解密，再绑定新 UUID 并原子保存；不输出凭据。
- 已捕获旧身份的活动扫描、同步与补全任务需要重新提交，原计划和审计保留。存在未完成的文件写回恢复时拒绝主库迁移，须先由旧版完成恢复。已安装旧版的多个本地音乐实例须先整理为单个实例的多目录配置，升级不会猜测合并它们。
