# 本地歌词与封面补全任务

独立 `musicfs.scraper.local` 插件读取当前来源的内嵌内容及宿主授权的同目录旁置文件。S-P6-03 手动任务先保存候选预览，用户选用后才改变当前内容并加人工锁；S-P6-04 另提供显式导入歌词和可配置的自动补缺。两者可在没有在线 Music Provider、FFmpeg 或指纹工具时工作；NCM 内嵌内容需要启用独立 NCM 解密插件。

## 配置和能力

在 YAML 的 `plugins` 中加入独立实例，路径相对于配置文件目录：

```yaml
- pluginId: musicfs.scraper.local
  executable: ../build/windows/musicfs-plugins.exe
  args: ['--plugin', 'scraper-local']
  enabled: true
  instances:
    - instanceId: scraper-local
      name: 本地歌词与封面
      role: scraper
      enabled: true
      config: {}
```

Linux 使用 `../build/linux/musicfs-plugins`，保留相同 `args`。可选 `sha256` 钉住可信程序；省略时采用明确路径与本次启动摘要检查。插件和实例都启用才可运行，配置修改重启生效，没有动态启停。最多16个配置实例；每个请求最多选择4个。Scraper 没有独立 `musicRoots`、凭据或导入默认项，读取授权来自目标曲目的 Provider，不能用配置扩大音乐根权限。

`GET /api/v1/scraper-instances` 返回本人实例的编号、显示名、字段和可用状态，不包含执行路径。`available` 表示已启用且程序通过本地校验，不保证每个源都支持提取。`queryCorrectionsSupported=false`：当前本地插件不进行在线搜索，提交非空标题/艺人修正返回415 `UNSUPPORTED`。

支持的格式和限制见[插件说明](../src/plugins/scraperlocal/README.md)：MP3 ID3v2.3/v2.4 的 USLT/APIC、原生 FLAC 的歌词注释/PICTURE，以及授权的同名歌词/图片和目录封面。NCM 由独立后台解密通道提供 MP3/FLAC 字节，不把 NCM 外层图片或不可靠元数据当作已验证内容。不支持的内嵌内容保留问题说明，仍可使用有效旁置内容。

## 搜索和预览

所有修改需要登录 Cookie、允许的 Origin 和 CSRF。创建任务另需8—128字符的 `Idempotency-Key`：

```http
POST /api/v1/metadata-jobs
Content-Type: application/json

{"sourceTrackIds":["来源编号"],"instanceIds":["scraper-local"],"fields":["lyrics","artwork"],"languages":[]}
```

单曲也使用1项 `sourceTrackIds`；显式批量最多64项，不接受重复来源。也可用 `sourceAlbum:{sourceAlbumId,expectedRevision}` 在入队事务展开1—64个固定成员，不能同时传非空 `sourceTrackIds`；详见[专辑流程](albums.md)。`fields` 为不重复的 `lyrics/artwork`；`instanceIds` 省略或空数组选择本人当前可用实例，再扣除可选 `excludeInstanceIds`。最终选择最多4项，较多实例时应显式指定。排除列表最多16项，不能把同一实例同时选中和排除。未知实例统一404，显式离线实例503；未选到可用实例415。

`languages` 最多16项，不接受空标签；省略或空数组不筛选。指定语言时只按已声明语言过滤，常见 ID3 代码作明确别名归一（例如 `zho→zh`、`eng→en`）。未知语言为 `und`，只有显式请求 `und` 才会匹配未知语言；不会从中文歌词正文猜语言。封面不参与语言过滤。没有匹配时保留问题说明，不伪造候选。

返回202 `{jobId,merged}`；`GET /api/v1/metadata-jobs/{jobId}` 读取状态及逐目标结果。状态为 `queued/running/completed/failed/cancelled`。有部分目标成功时总任务可为 `completed`，必须查看每个目标的状态、`failureCode` 和 `issues`；搜索成功但没有候选也是 completed，不代表已找到内容。重新尝试失败项时，用这些来源编号、新幂等键及需要的实例/排除列表创建新任务。

候选包含来源/资产版本、搜索时修订、实例、字段、语言、格式、大小、摘要、来源依据和有效期，不含正文、路径或任务资源票据。`origin=embedded|sidecar`，`basis` 如 USLT、PICTURE、exactStem、directoryCover，说明候选来自哪里，不是歌曲匹配置信度。可用候选携带 `previewObjectId`，通过已有 `GET /media/{id}` 和 `/info` 授权预览；失败候选的预览编号为 null。搜索不会更新当前选择。

后台默认1个目标并发，每个目标总期限30秒。每目标最多64候选、每任务最多256候选和32 MiB预览预算；单份内容仍遵守歌词1 MiB、图片8 MiB和解码限制。原音频、旁置内容及任务代次在发布前复核，晚完成结果不能绕过取消或版本变更。独立 NCM 后台槽不占播放或指纹的 NCM 准入槽。

## 人工选用和取消

先从媒体选择接口读取最新 `revision`，再提交：

```http
POST /api/v1/metadata-candidates/{candidateId}/apply
Content-Type: application/json

{"expectedRevision":0}
```

选用必须带幂等键。服务复核所有者、当前资产、候选代次、任务状态、30分钟有效期及预览文件后，使用与人工上传相同的内容选择事务，并设置字段/语言人工锁。旧修订返回409；客户端不能指定另一个对象、来源或 `automatic`。

候选的 `target.revision` 是搜索时快照。先选歌词使修订变为1后，可以明确用修订1继续选封面；不能用旧修订0覆盖后来的选择。已成功操作按原键重放，不再次修改当前选择。任务创建和人工选用的幂等窗口为24小时。

`POST /api/v1/metadata-jobs/{jobId}/cancel` 接受 `{}`，自然幂等，不需要幂等键。已完成任务也可取消，从而撤销其候选的新选用资格；逐目标已完成结果仍供解释。取消不撤销已发布的选择和人工锁，不能用新键继续应用该任务候选；此前成功应用的原键仍可重放。人工解锁沿用[内容接口](media.md)。

停用插件不会删除已经保存的内容、人工锁或历史任务。当前没有 Web 补全页面、远端 Scraper 或自动合并录音。补全本身不改原件；另行启用并显式提交的[写回任务](writeback.md)使用当前选用内容。协议控制面与二进制通道边界见[插件契约](contracts/plugins/v1/README.md)，手动任务实测范围见[验收记录](validation/metadata-http.md)。

## 导入歌词与自动补缺

Local Files 的 `local-import-v2` 声明允许 `importLyrics=true`。可设在实例的 `importDefaults`，也可在本次扫描的 `options` 覆盖。扫描只对已授权根和实际递归深度内成功观察的来源创建持久触发记录；未扫描的子目录不顺带导入。触发绑定扫描代次、来源和资产版本，失败/取消的扫描不能冒充成功导入。

显式导入只补歌词，全局 `metadata.automatic.enabled=false` 也会执行；全局自动策略可另行选择歌词、封面、语言和实例顺序。具体配置见[配置说明](configuration.md#自动补齐策略)。两种原因分别记录为 `importLyrics` 和 `automatic`；全局关闭对应记录为 skipped，没有隐藏的自动选用。

`GET /api/v1/scan-jobs/{id}/metadata-results?limit=50&cursor=...` 返回 `{items,nextCursor}`，只限当前用户；limit 为1—200，默认50。游标只可用于原扫描，错误或跨扫描游标为410，别人的扫描为404。扫描状态 succeeded 只表示曲库扫描完成，应继续查看此接口各项的 `state`（queued/preparing/running/completed/skipped/failed）、`jobId`、`failureCode` 和 `actions`；`jobId` 可进一步读取原候选任务与问题。没有可用Scraper时，显式导入明确 failed，不报告空的成功。

每项 action 说明字段/语言、applied/skipped/failed 等状态及原因，成功时给出 bindingId；不泄漏源路径、内部策略、资源票据或内部修订。completed 仍可能包含跳过或失败的 action，不能仅看总状态。常见跳过原因：manualLocked（人工锁）、existingContent（已有可用内容）、ambiguous（同优先实例的同槽候选摘要冲突）、noEligibleCandidate（没有明确属于当前来源的候选）。

自动选用仅接受本地内嵌内容或同名旁置内容，不自动选目录通用封面；按实例顺序选候选，不靠文件顺序打破内容歧义。只补空或不可用且未锁定的字段/语言，不覆盖现有可用内容，也不自动加人工锁。人工选用后再扫描，即使旁置文件变化，也保留已选对象和人工锁。任务发布复核资产、候选与文件状态，重启不会把已成功选用重复执行成新修订。源音频和旁置文件始终只读。

自动流程的真实服务验证见[自动补全验收](validation/metadata-automatic.md)。停用Scraper后仍能读取已有内容和结果；新导入任务按不可用失败，普通播放保持独立。

完整结构化搜索与取回记录保存在独立文件，通过任务的 /documents 接口读取；封面可生成256像素 PNG 缩略图，见[派生媒体](derived-media.md)。
