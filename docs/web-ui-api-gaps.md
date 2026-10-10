# Web 页面接口交付与接入边界

核对日期：2026-10-10。本轮完成下列 Server 接口与插件管理能力；2026-10-11 停用调整为重启生效，Web 新增能力变更订阅。本页记录最终功能范围和待接入边界；精确字段以 [OpenAPI](contracts/openapi.yaml) 和 [HTTP Schema](contracts/http.schema.json) 为准，验证状态见[实施记录](validation/progress.md)。

依据：[Web 开发方案](../../web/docs/react-development.md)、[搜索设计](../../docs/ui-search.md)、[曲目列表设计](../../docs/ui-tracks.md)、当前 `docs/contracts/openapi.yaml` 与 `http.schema.json`。

## 1. 本轮交付

| 需求 | 已实现接口及说明 |
| --- | --- |
| 本地曲目、专辑、歌手搜索 | `/library/search`、`/source-artists/{id}`；[完整条件与身份](catalog-query.md) |
| 全库分类、组合过滤及排序 | `/library/facets`；查询支持歌手、目录、格式、流派、年份、采样率、收藏、观测可用性及码率 |
| 固定查询全部结果、恢复和取消 | `/library/queries`、`/library/queries/{id}`；持久快照独立于后续扫描，分页不截断后冒称完整 |
| 在线四类搜索及详情 | `/online-instances/{id}/search?kind=...`，专辑／歌手详情和相关曲目／专辑分页；见[在线说明](online.md) |
| 本人歌单搜索 | `/playlists?q=...`；按名称过滤，保留原键集分页语义 |
| 在线歌单曲目展示 | `/online-instances/{id}/playlists/{externalId}/tracks` |
| 批量展示来源引用 | `/source-refs/lookup`、`/online-instances/{id}/tracks/lookup`；顺序及重复引用保留，逐项失败 |
| 个人资料、收藏、最近播放 | `/me/profile`、`/me/favorites`、`/me/recently-played`；[权限、去重与期限](personal-library.md) |
| 插件管理、配置、动态启用与重启停用 | `/admin/plugins`、实例状态／配置、操作查询；见[插件管理](implementation/plugin-management.md) |
| 插件能力变更通知 | `/events` 的 `capabilities.changed` SSE；前端重新读取本人可见的来源与能力 |

## 2. 保留的能力边界

`playableOnly` 没有可靠的静态判定，显式列为不支持；`availableOnly` 只表示扫描观测可用，不能以此承诺音频解码或播放授权。搜索结果的“播放全部”使用固定本地查询，逐曲播放仍重新校验权限和源版本。在线资料仍不构成在线音频播放许可。

本地专辑是显式 `SourceAlbum`，不会按同名标签自动合并；本地歌手是来源内署名身份。在线全部来源搜索保留各来源独立游标／错误／顺序，由 Web 编排；没有把多个平台相关性分数混为统一排名。头像上传、跨设备搜索记录和在线查询固定全部结果尚未实现。

本轮 Web 只新增能力变更订阅与现有来源列表刷新。四类搜索、收藏、历史、完整查询和批量展示的页面适配仍需单独实施；不能把已有页面占位视为接口缺失，也不能把接口存在视为 UI 已交付。

## 3. 对接验收要求

1. Web 按已发布的 OpenAPI 和单一 Schema 更新适配器，保持权限、能力、身份、版本、分页和错误语义；响应使用真实序列化内容进行契约验证。
2. 覆盖无结果、无权限、离线、限流、部分来源失败、追加失败、旧查询晚到、游标过期、未知总数。
3. 曲目／专辑／歌手／歌单同名不能作为跨来源合并依据；可播放性与资料可见性分别表达。
4. 真实端到端验收后再将等待中的界面标记为已实现；Mock 测试只证明页面行为。
5. 最近播放在真实播放开始后上报，校验当前登录会话、播放会话和尝试；遵守去重、清理与保留期限。收藏与资料使用服务端账号隔离数据。
6. SSE 首次连接与重连均重读本人可见的来源列表；注销关闭连接。插件状态写入先返回操作收据，须查询操作终态后展示实际应用结果。
