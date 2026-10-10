# P6 专辑、发行及派生媒体接口验收

日期：2026-10-10。范围为来源专辑/发行管理、独立封面、专辑补全任务快照，以及完整刮削文档和缩略图的真实 HTTP 读取；没有扩展最简 Web。使用 Windows/amd64、Go 1.26.9，仓库依赖为 yaml v3.0.5、x/sys v0.49.0、x/text v0.43.0、SQLite v1.60.1、JSON Schema v6.0.3；本轮没有改动依赖。

## 接口与事务证据

- `tests/contract/api/albums_http_test.go` 通过真实 SQLite 和内容文件验证同名实体独立、成员/修订严格输入、登录/Origin/CSRF、本人 Provider 授权（包括空专辑）、其他所有者404、分页、原键重放及异参数冲突。来源专辑和发行分别选用、锁定、解锁封面，成员编辑推进引用发行修订但不清封面，来源媒体选择修订不受影响。损坏的新预览不能覆盖原选择。
- 同一 HTTP 测试验证缩略图生成和重复调用、真实PNG解码、小图不放大、描述与正文，以及私有原图片不可访问。`metadata_http_test.go` 增加私有任务文档404、专辑目标必需修订、null/未知字段/混合目标拒绝。
- `tests/integration/storage/enrichment_album_test.go` 验证入队同一事务保存成员、资产和媒体选择修订；随后编辑专辑、改变来源资产，再用原键得到原任务。旧专辑修订的新请求冲突，改变参数复用键冲突；重开数据库保持快照。空专辑和65成员拒绝，64成员成功，其他所有者和混合目标拒绝。
- HTTP Schema 验证真实序列化响应与有效/无效样本。OpenAPI 的新增路由、Schema引用、CSRF和幂等声明通过项目结构检查；不将其称为官方 OpenAPI 元规范认证。新缩略图 POST 明确自然幂等，未虚构幂等键重放。

接口与样本初轮通过4.981秒；加入实际 Provider 授权和缩略图读取后6.351秒。专辑快照存储首轮通过1.410秒。成员模型随后按提交顺序保留，最终回归使用修正后的实现。

## 真实服务流程

`TestAlbumsActualServerSnapshotDocumentsThumbnailAndRestart` 构建并启动正式 Server 组合、Local Files 和 Local Scraper 子进程，使用独立临时 YAML/数据库/媒体目录。只读取仓库合成 `markers.wav`，复制成两条来源并生成测试 LRC/PNG，未访问用户歌曲、外部账号或在线音乐。

1. 扫描两条来源，按显式顺序创建来源专辑并创建引用它的发行；检查返回的成员顺序。
2. 按专辑编号/修订创建歌词与封面补全任务，确认两条固定目标含资产版本和选择修订；搜索未自动改变当前选择。
3. 编辑专辑为单成员，使专辑及发行修订推进；原请求/原键仍返回原两成员任务，旧修订的新键被拒绝。
4. 从仍在专辑内的成员预览分别选用专辑和发行封面，确认独立人工锁，来源曲目的选择修订保持0。
5. 读取每个来源的完整刮削文档，检查原专辑请求、整个任务目标、真实插件版本、资源局部编号、结构化搜索/取回结果和候选。正文为JSON附件，不含原目录路径、资源访问票据或内部通道引用。
6. 从预览生成缩略图；重复生成复用有效对象，实际PNG尺寸不超过原图且≤256。随后将专辑成员置空。
7. 停服并禁用Scraper后用原YAML数据目录重启，原封面及锁、任务快照、完整文档和缩略图仍可读取；读取合成WAV的实际单/多Range，正常Stop。逐个比较测试音频、歌词和图片摘要，确认均未修改。

普通真实服务测试通过，包耗时5.606秒（测试主体5.53秒）。首次编译发现测试代码使用了错误的Go字段名，已改为实际DTO的 `ID`；这是测试实现错误，没有将该次编译算作通过。

## 最终并发回归

使用 Windows UCRT64 C 工具链与 `-race`，对本轮存储、API模型/权限及真实服务流程执行一次联合定向回归：

| 范围 | 结果 | 包耗时 |
| --- | --- | --- |
| 专辑任务事务快照、重放、重启、数量与所有者边界 | 通过 | 9.186秒 |
| 专辑/派生HTTP、专辑目标输入、完整Schema与OpenAPI结构 | 通过 | 28.318秒 |
| 真实专辑任务、完整文档、缩略图、禁用重启和Range播放 | 通过 | 13.566秒 |

联合定向命令为 `go test -mod=readonly -race ./tests/integration/storage ./tests/contract/api ./tests/e2e -run 'TestEnrichmentAlbum|TestAlbumsHTTP|TestAlbumOpenAPI|TestMetadataHTTP|TestAllModelsCompile|TestContractSamples|TestOpenAPIReferencesAndOperations|TestAlbumsActualServerSnapshotDocumentsThumbnailAndRestart' -count=1`。原生进程/媒体文件发布使用获准的沙箱外执行，全部素材和输出限制在测试临时目录。

本记录只声明上述 Windows 定向验收。全量 Windows、Linux 及派生存储崩溃/回收/备份的统一结果另见[专辑与派生媒体验收](albums-media.md)。没有新增浏览器界面或浏览器实测，没有代替人工听音，也不声明 ARM、macOS 或 NAS 验收。公开用法见[专辑说明](../albums.md)和[派生媒体说明](../derived-media.md)。
