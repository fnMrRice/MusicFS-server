# 在线来源的匿名资料接口

S-P7-02 提供三个独立 Go Provider 程序的配置与公开资料接口。当前没有平台账号登录、Cookie/token 导入、订阅或地区权益验证，也没有经过授权验证的整曲音频资源。最简 Web 仍只负责本地列表和播放；在线操作通过 API 验收。

## 配置和实例状态

构建脚本同时生成 `musicfs-plugin-netease`、`musicfs-plugin-qqmusic`、`musicfs-plugin-applemusic`。Windows 程序有 `.exe` 后缀。示例默认禁用三个在线插件；需要读取公开资料时，显式启用插件及实例：

```yaml
- pluginId: musicfs.netease
  executable: ../build/windows/musicfs-plugin-netease.exe
  enabled: true
  instances:
    - instanceId: netease-public
      name: 网易云公开资料
      role: provider
      enabled: true
      config: {}
```

QQ 和 Apple 对应 `musicfs.qqmusic`、`musicfs.applemusic`。平台由程序身份固定，实例不能通过请求改变平台。配置只接受空 `config`，拒绝上游 URL、代理、任意参数、Cookie、凭据、账号编号、musicRoots 和本地扫描导入选项。路径相对 YAML 所在目录，程序可配置 SHA-256；修改配置重启生效，没有动态启停。三平台合计最多16个在线实例，账号范围固定为空字符串，表示匿名，不表示默认登录用户。

`GET /api/v1/online-instances` 只列出本人配置实例，含 `instanceId/name/pluginId/platform/accountId/availability` 及安全能力/空配置声明。`available` 只表示工作进程已通过校验并仍运行，每项能力另看 `supported`；程序丢失、禁用、摘要或握手错误则为 offline。列表不公开执行路径、运行目录、配置摘要或平台凭据。

| 平台 | 当前实际范围 | 明确限制 |
| --- | --- | --- |
| 网易云 | 匿名曲目搜索、资料、公共推荐第一页和公开歌单条目观测已实测 | 不登录、不读取私人歌单、不解析音频；上游无固定快照版本 |
| QQ | 匿名搜索适配存在，可返回真实上游结果/失败 | 最近核心验收收到 HTTP 500，因此没有 supported 能力声明；资料、推荐和歌单不支持 |
| Apple Music | 声明目录需要开发者授权 | 未接入凭据，目录操作返回401 AUTH_REQUIRED；没有 MusicKit/preview/完整音频交付 |

各次外网验证的具体时间和差异见[核心证据](validation/online-core.md)及[HTTP组合验收](validation/online-http.md)。某次成功不是持续可用承诺，失败也不会被转换成空的成功结果。

## 查询接口

以下接口均需要 MusicFS 登录 Cookie。所有 ID 是平台不透明编号，不能使用 URL 或服务器文件路径。未知实例和其他用户实例统一404。参数区分大小写，未知或重复参数、非法类型和超过限制均400。

| 方法与路径（均以 `/api/v1` 开头） | 输入 | 返回 |
| --- | --- | --- |
| GET `/online-instances/{id}/search` | `q` 必需，去两端空白后1—256 UTF-8字节；limit默认20、1—50；可选cursor | `tracks/nextCursor/truncated/consistency/observedAt` |
| GET `/online-instances/{id}/tracks/{externalId}` | 平台曲目编号 | 标题、艺人、专辑、可空时长、平台资料页及元数据摘要 |
| GET `/online-instances/{id}/recommendations` | limit默认20、1—50 | 公共歌单推荐、观测时间和120秒有效期；`personalized=false,nextCursor=null` |
| GET `/online-instances/{id}/playlists/{externalId}` | 公开歌单编号 | 条目、`complete/consistency/metadataDigest/observedAt` |
| POST `/online-instances/{id}/tracks/{externalId}/resolve` | `{}`；另需允许Origin和CSRF，无幂等键 | 当前所有可用平台均415 UNSUPPORTED，不生成会话或返回音频地址 |

搜索游标绑定用户、实例、平台、规范查询和limit；有效期5分钟，进程重启也失效。游标不能跨实例或改查询使用。它表示继续读取远端分页，不固定远端快照；需要完整列表时由调用方检查 `nextCursor`、`truncated` 及远端变化风险，不能把两页视为同一确定版本。

曲目 `metadataDigest` 是规范元数据的摘要，不能当作音频资产版本、文件 SHA-512 或识别证据。`playbackAvailability=unknown`，`sourceUrl` 是固定平台资料页。接口不返回 SourceAsset、streamUrl、请求私密头或播放SDK句柄，不把目录preview当作完整裸流。

歌单最多10000条，按这次观测的 `position` 保留顺序和重复曲目。`complete=true` 仅说明返回数与本次申报总数相等；`consistency=unversioned` 明确没有远端固定版本。公开推荐和歌单查询不会导入个人曲库、创建媒体资产或据缺失条目删除本地数据；显式导入和持久同步属于独立操作。

## 资源与失败边界

宿主同时最多4个在线查询，每次最多10秒；超额返回429和秒单位的 `retryAfter/Retry-After`，不创建无界等待。每个工作进程另有2个RPC并发限制，以及复用的每秒一次匿名网络准入。上游429/业务限流会维持冷却，不隐藏重试；调用方应按提示有界重试。断连或取消传递到工作进程请求。

网络仅使用程序固定的 HTTPS 主机与路径，校验DNS公网地址、固定已校验目标连接并验证TLS，不跟随重定向、不继承代理/Cookie和账号环境。正式入口不能通过配置或RPC替换测试Transport。程序仍是管理员选择的受信任本地可执行文件，进程生命周期隔离不等于操作系统网络/文件沙箱。

离线为503 SOURCE_OFFLINE，上游异常为503 TEMPORARY_FAILURE，当前未实现能力为415 UNSUPPORTED，Apple目录授权缺失为401 AUTH_REQUIRED。最后一种表示平台授权尚未接入，不能用本地登录凭据替代。公开错误不携带上游原始正文、网络地址参数或账号秘密；已有本地曲库和播放流程保持独立。

按来源聚合读取、部分失败与缓存刷新见[公开推荐](recommendations.md)；该接口同样不隐式导入曲库。公开模型见 [OpenAPI](contracts/openapi.yaml)、[HTTP Schema](contracts/http.schema.json)；独立进程契约见 [online.schema.json](contracts/plugins/v1/online.schema.json)。没有新增Node运行依赖，参考来源及许可核对见[平台调查](platform-capabilities.md)。
