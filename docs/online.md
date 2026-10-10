# 在线来源的账号资料接口

S-P7-02 提供三个独立 Go Provider 程序的配置与公开资料接口。后续[平台账号登录增量](platform-login.md)已接入登录控制面及 Web 设置入口，网易/QQ 二维码创建实测通过，真实用户授权仍待验收。本页所有业务接口要求已校验的平台账号；网易云使用带会话的客户端 EAPI、QQ 搜索使用桌面 RPC，拒绝匿名回退；尚无订阅或地区权益验证，也没有经过授权验证的整曲音频资源。

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

QQ 和 Apple 对应 `musicfs.qqmusic`、`musicfs.applemusic`。平台由程序身份固定，实例不能通过请求改变平台。配置只接受空 `config`，拒绝上游 URL、代理、任意参数、Cookie、凭据、账号编号、musicRoots 和本地扫描导入选项。路径相对 YAML 所在目录，程序可配置 SHA-256；已配置的插件和实例支持管理员动态启停，显示名称支持在线修改；执行程序路径、程序摘要和平台身份仍由部署配置管理。三平台合计最多16个在线实例，业务使用对应实例当前已校验的账号；实例列表在未登录/待校验时显示 loginRequired，账号 ID 仅在已校验时返回。

`GET /api/v1/online-instances` 只列出本人配置实例，含 `instanceId/name/pluginId/platform/accountId/availability` 及安全能力/空配置声明。`available` 表示工作进程已通过校验、仍运行且平台账号已校验，每项能力另看 `supported`；程序丢失、禁用、摘要或握手错误则为 offline。列表不公开执行路径、运行目录、配置摘要或平台凭据。

| 平台 | 当前实际范围 | 明确限制 |
| --- | --- | --- |
| 网易云 | 现有四类搜索、资料、推荐与歌单观测已迁移至带账号会话的 EAPI；合成测试通过，待实号验收 | 必须登录；不解析音频，上游无固定快照版本 |
| QQ | 搜索改用带账号凭据的桌面 RPC；待实号验收 | 必须登录；仍不标为已验证能力，其他资料、推荐和歌单不支持 |
| Apple Music | MusicKit 登录控制面已接入，待实号验收 | 必须登录；目录查询尚未实现，登录后返回 UNSUPPORTED；没有完整音频交付 |

各次外网验证的具体时间和差异见[核心证据](validation/online-core.md)及[HTTP组合验收](validation/online-http.md)。某次成功不是持续可用承诺，失败也不会被转换成空的成功结果。

## 查询接口

以下接口均需要 MusicFS 登录 Cookie。所有 ID 是平台不透明编号，不能使用 URL 或服务器文件路径。未知实例和其他用户实例统一404。参数区分大小写，未知或重复参数、非法类型和超过限制均400。

| 方法与路径（均以 `/api/v1` 开头） | 输入 | 返回 |
| --- | --- | --- |
| GET `/online-instances/{id}/search` | `q` 必需，去两端空白后1—256 UTF-8字节；`kind=tracks\|albums\|artists\|playlists` 默认tracks；limit默认20、1—50；可选cursor | `kind`及对应数组；其他类型数组为空，另含`nextCursor/truncated/consistency/observedAt` |
| GET `/online-instances/{id}/tracks/{externalId}` | 平台曲目编号 | 标题、艺人、专辑、可空时长、平台资料页及元数据摘要 |
| GET `/online-instances/{id}/albums/{externalId}` | 平台专辑编号 | 专辑实体、歌手、可空曲目数、封面引用和来源页 |
| GET `/online-instances/{id}/artists/{externalId}` | 平台歌手编号 | 歌手实体、可空作品计数、头像引用和来源页 |
| GET `/online-instances/{id}/albums/{externalId}/tracks` | limit默认20、1—50；可选cursor | `kind=tracks`的页；游标绑定本次专辑完整观察摘要，最后一页truncated表明观察不完整 |
| GET `/online-instances/{id}/artists/{externalId}/albums` | limit默认20、1—50；可选cursor | `kind=albums`的页，保留上游专辑顺序，不固定远端快照 |
| GET `/online-instances/{id}/artists/{externalId}/tracks` | limit默认20、1—50；可选cursor | `kind=tracks`的页，固定上游hot排序，不固定远端快照 |
| POST `/online-instances/{id}/tracks/lookup` | `externalIds` 数组，1—50项；允许Origin和CSRF，无幂等键 | 保持输入顺序与重复，缺失逐项`NOT_FOUND`；只使用一次上游批量详情请求 |
| GET `/online-instances/{id}/recommendations` | limit默认20、1—50 | 公共歌单推荐、观测时间和120秒有效期；`personalized=false,nextCursor=null` |
| GET `/online-instances/{id}/playlists/{externalId}` | 公开歌单编号 | 条目、`complete/consistency/metadataDigest/observedAt` |
| GET `/online-instances/{id}/playlists/{externalId}/tracks` | limit默认20、1—50；可选cursor | 位置、来源ID、标题／歌手／时长等资料，缺失项保留；每页一次歌单观测和一次批量详情请求 |
| POST `/source-refs/lookup` | `sources` 是1—50项个人歌单`SourceRef`；允许Origin和CSRF，无幂等键 | 本地`SourceTrack`或远程来源及资料；顺序、重复和单项错误保留，不遍历整个曲库 |
| POST `/online-instances/{id}/tracks/{externalId}/resolve` | `{}`；另需允许Origin和CSRF，无幂等键 | 当前所有可用平台均415 UNSUPPORTED，不生成会话或返回音频地址 |

搜索游标绑定用户、实例、平台、kind、规范查询和limit；有效期5分钟，过期返回`CURSOR_EXPIRED`，进程重启后签名失效。伪造、跨实例或改查询使用返回`INVALID_ARGUMENT`。它表示继续读取远端分页，不固定远端快照；需要完整列表时由调用方检查 `nextCursor`、`truncated` 及远端变化风险，不能把两页视为同一确定版本。歌单曲目分页另绑定本次完整条目观测摘要；下一页观测改变返回`CURSOR_EXPIRED`，前端须刷新首屏，不拼接新旧结果。

专辑和歌手外部身份必须与实例ID一起使用，不能仅按同名合并。歌单含可空创建者和封面；专辑含歌手、曲目数、封面，歌手含作品计数和头像。图像引用只保留平台`*.music.126.net`的HTTPS地址，Server不会据此后台抓取文件；Web需依据自己的CSP策略决定是否显示。专辑／歌手详情与关联作品的三条分页均已提供接口，仍不代表Web已经接入详情页面。专辑与歌单分页成员摘要忽略封面CDN域名轮换，避免同一观察被误判为集合变化。

来源引用批量展示先按当前用户查询，未知和其他用户来源均为`NOT_FOUND`；在线条目按实例分组，一组一次批量调用，不逐首访问上游。在线离线、禁用、限流等错误保留在对应条目，不影响本地条目。资料是当前读取值，不声称与过去的个人歌单快照具有相同修订。本人歌单`GET /playlists?q=...`同时支持Unicode小写后的名称包含匹配；过滤作用于完整本人歌单集，沿用`after` ID键集分页，实时更新可能影响后续页，改变关键词时应从第一页开始。

曲目 `metadataDigest` 是规范元数据的摘要，不能当作音频资产版本、文件 SHA-512 或识别证据。`playbackAvailability=unknown`，`sourceUrl` 是固定平台资料页。接口不返回 SourceAsset、streamUrl、请求私密头或播放SDK句柄，不把目录preview当作完整裸流。

歌单最多10000条，按这次观测的 `position` 保留顺序和重复曲目。`complete=true` 仅说明返回数与本次申报总数相等；`consistency=unversioned` 明确没有远端固定版本。公开推荐和歌单查询不会导入个人曲库、创建媒体资产或据缺失条目删除本地数据；[显式导入和持久同步](remote-playlists.md)使用独立操作。

## 资源与失败边界

宿主同时最多4个在线查询，每次最多10秒；超额返回429和秒单位的 `retryAfter/Retry-After`，不创建无界等待。每个工作进程另有2个RPC并发限制，以及复用的每秒一次平台网络准入。上游429/业务限流会维持冷却，不隐藏重试；调用方应按提示有界重试。断连或取消传递到工作进程请求。

网络仅使用程序固定的 HTTPS 主机与路径，校验DNS公网地址、固定已校验目标连接并验证TLS，不跟随重定向、不继承代理/Cookie和账号环境。正式入口不能通过配置或RPC替换测试Transport。程序仍是管理员选择的受信任本地可执行文件，进程生命周期隔离不等于操作系统网络/文件沙箱。

离线为503 SOURCE_OFFLINE，上游异常为503 TEMPORARY_FAILURE，当前未实现能力为415 UNSUPPORTED，平台未登录或待校验为403 PLATFORM_AUTH_REQUIRED；不会退出 MusicFS 会话。公开错误不携带上游原始正文、网络地址参数或账号秘密；已有本地曲库和播放流程保持独立。

按来源聚合读取、部分失败与缓存刷新见[公开推荐](recommendations.md)；该接口同样不隐式导入曲库。公开模型见 [OpenAPI](contracts/openapi.yaml)、[HTTP Schema](contracts/http.schema.json)；独立进程契约见 [online.schema.json](contracts/plugins/v1/online.schema.json)。没有新增Node运行依赖，参考来源及许可核对见[平台调查](platform-capabilities.md)。

## 动态运行状态

禁用立即停止接收该实例的新查询，返回`PLUGIN_DISABLED`；drain允许已准入的查询和整项歌单同步完成。歌单同步从运行时校验、上游观测到快照发布或失败记录共用一次任务租约，停机须等这些步骤释放资源。force取消已准入请求，但只有实际清理和工作进程退出后才完成停机；其他实例继续独立运行。

重新启用会建立新的工作进程和运行代次，旧运行时不能借用新代次的准入许可。推荐仅清理发生变化实例的缓存和共享请求，迟到的旧结果不会覆盖新资料；禁用或进程死亡后不把过去的成功缓存标成可用。`GET /api/v1/events`通过登录Cookie提供`capabilities.changed`事件，连接及重连会收到当前修订，调用方据此重新读取实例和能力列表；事件本身不携带部署路径或完整配置。
