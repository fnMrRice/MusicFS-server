# S-P6-05 显式写回：独立模块验收

本页记录 `src/metadata/writeback` 的独立能力；配置、API、持久 SQL 任务、library 协调和稳定来源映射由组合层接入后，才能宣称整步完成。

## 当前范围

- `Policy.Enabled` 缺省为 `false`，按 owner、实例、根、字段授权；任务绑定 SourceTrack、SourceAsset、观测版本、文件身份和媒体选择修订。
- 只支持 Windows 固定本地盘上的 NTFS、单链接普通 FLAC 文件。拒绝 Linux、网络盘、reparse 路径、只读文件、硬链接、ADS、EA、压缩、加密、稀疏和云文件属性；不把不支持解释为成功。
- 能力名为 **recoverableReplace**：两次按句柄改名各自原子，原件移到同目录备份后会出现短暂缺名窗口；不宣称单次 `atomicReplace`。
- 目录链持有禁止删除共享的句柄，防止根或中间目录被替换。源句柄禁止其他写入和删除；在用文件或无法获取保护时拒绝。
- 复制创建时间、owner/group/DACL 与 DACL 继承设置。不是通用 NTFS 元数据复制器；不宣称保留定制 SACL 审计策略等未验证语义。原备份保留原文件对象，未经用户安排不会清理。

## FLAC 编辑语义

依据 [RFC 9639](https://www.rfc-editor.org/rfc/rfc9639.html) 解析原生 FLAC 元数据。只设置显式提供的非空字段，不提供清空操作：

| 字段 | FLAC 表示 | 默认行为 |
| --- | --- | --- |
| title | TITLE | 已有非空值则保留 |
| artists | 多个 ARTIST | 已有非空值则保留 |
| album | ALBUM | 已有非空值则保留 |
| lyrics | LYRICS；识别已有 UNSYNCEDLYRICS | 已有非空值则保留 |
| artwork | PICTURE 的 front cover/type 3 | 已有封面则保留 |

`Overwrite=true` 才替换所选字段。其他评论的原始大小写、重复项、vendor、未选图片及其他 block 正文不变；STREAMINFO 和全部音频区逐字节复制。图片只接受完整解码验证的 PNG/JPEG，不下载 URL。元数据总量最多 32 MiB、评论正文最多 1 MiB、图片最多 8 MiB；不支持的畸形格式直接拒绝。

新文件写完后重新解析，核对音频区 SHA-512 与复制输入一致；独立模块返回完整文件 SHA-512 供写回恢复和审计，本身不发布后台证据。组合层在保护租约内完成新资产事务时，以专属 `writeback-full-file-v1` 配置修订发布 verified 完整摘要，不伪装为后台 hash 任务。没有音频解码或转码，也不宣称检查了每一音频帧的内部正确性。

## 持久阶段与恢复

`prepared → written → displaced → installed → committed`；不完整准备可 `aborted`，检测外部变化则 `blocked`。

1. 协调器先核验授权、资产和选择修订，取得独占租约；先持久化准备意图，再创建同目录 `.musicfs-writeback-<id>.new`。
2. 验证新文件后持久化其身份、版本、完整摘要和音频区摘要。
3. 再核验宿主状态，将受保护原文件按句柄改名为 `.musicfs-writeback-<id>.bak`。从不覆盖已有备份。
4. 将受保护新文件按句柄安装至空出的名称；如果外部进程已创建目标，停止并保留原备份，绝不覆盖新占用者。
5. 持久化 installed 后，由宿主 `Lease.Commit` 幂等完成数据库新资产和身份映射，然后持久化 committed。提交失败不能返回成功，恢复可重试。

`FileJournal` 提供真实文件日记，JSON 不含歌词/图片正文；生产可用同一 `Journal` 接口换成 SQLite。`Pending(after, limit)` 最多 128 条，排除 committed/aborted，但持续返回需人工处理的 blocked，以便重启后恢复来源屏障；遍历目录有 100000 项硬上限。服务进程互斥由宿主保证。

恢复重新取得独占租约，按文件身份、全文件摘要、大小及可判定的版本识别原件、新件、备份。改名会改变 NTFS change time；在文件操作完成但阶段未落盘的窗口，使用稳定文件 ID 加完整摘要识别已移动对象。已落盘 installed 的版本必须精确匹配。未知文件、外部修改、授权撤销均不自动覆盖或回滚；保留备份供明确处理。prepared 阶段的半成品保留审计，无变化的完整 staging 按受保护句柄删除。

此验收覆盖服务进程意外退出；不把测试结果扩大为任意硬件掉电、损坏磁盘或 NAS 的持久性保证。

## 宿主必须实现的边界

- `Coordinator.Acquire/Recover` 核验 owner 与完整目标归属，不能信任调用者提供路径。独占租约覆盖扫描、hash、scrape、播放读取及新任务进入；活动播放应延期。
- `Lease.Check` 在准备、替换、数据库提交前校验 expected asset/selection revision。恢复必须可识别已提交同一 job 的幂等重放。
- `Lease.Commit` 按 job ID 幂等地保留 SourceTrack/Recording/关联，更新新文件身份、创建新 SourceAsset，失效旧 hash/证据并重新探测；若发布 Result 摘要，必须仍持保护租约并使用写回专属证据修订。
- 处于 displaced/installed/blocked 的来源应保持恢复屏障，不能释放到普通扫描而把内部临时缺名或新 inode 当成无关来源。
- Root.Identity、Target.FileIdentity、FileState.Version 与正式 SourceAccess 完全同构：volume serial/file index，和 `SHA256(identity:size:mtimeNs:changeTime)`。
- 启动恢复应先于读取和扫描后台任务；此独立模块没有自行启动后台进程、启用开关或修改 library 数据。

## 已验证

主机协调增量已接入 library、playback 和官方本地插件。provider 级读租约覆盖完整扫描发布、Open/Hash/Scrape 至 Revoke，以及播放会话至停止、撤销、到期或关闭。排他写回遇占用立即延期；未解决日记可恢复 provider 屏障。上下文取消不会在文件清理完成前提前释放已取得租约。

本地插件通过仅宿主可调用的 RebindLocalTrack，将原路径的稳定 externalID 从旧文件身份移至新身份；三项状态修改有独立持久意图，部分写入后重启会拒绝扫描和解析，恢复只接受精确的前值或已写入后值。此协调仅约束本服务，不阻止外部程序在未受系统句柄保护的时间修改文件。

InspectCapability 只读检查真实根目录、固定本地 NTFS 和目录 ADD_FILE 权限，返回 recoverableReplace；不依据 Windows 平台名称直接宣称可用。改名必须另取得源和暂存文件自身 DELETE 权限，不额外要求可绕过子文件 ACL 的目录 DELETE_CHILD；普通 Modify ACL 可用。开启授权与逐文件执行检查仍独立。

- 真实 NTFS：默认关闭、根/字段授权、过期修订、在用、只读、硬链接、ADS、文件大小和根身份变化均拒绝，原件字节不变。
- 中文和空格路径；编辑前后音频区、STREAMINFO 相同；真实 FFmpeg 解码 PCM 相同；未知评论/重复项/应用与保留 block 保留。
- written/displaced/installed/committed 前后注入日记失败；真实子进程在前三个阶段落盘后退出，再打开日记恢复成功；数据库失败后恢复只提交一次。
- 源保护期间外部写入/改名失败；缺名窗口外部创建目标时停止；重启后目标外部修改也不被覆盖。
- 并发重放、异参同 ID 拒绝、取消、暂存失败、无变化不替换、恢复分页；损坏格式、空字段、图片 URL、短写拒绝。

验证只操作测试目录里的合成 FLAC 副本，不读取或改写私人音乐库。受限 Windows 沙箱禁止真实 hardlink/rename，因此文件测试在批准的常规进程权限下执行。

定向结果：Windows 全包 race 3.852s 通过；`go vet` 通过；Linux amd64 测试程序交叉编译通过（不代表该平台允许写回）。

协调增量定向 race：library 6.999s、playback 19.799s、真实插件 18.663s、NTFS 写回 2.496s 通过，覆盖扫描发布占用、缓存播放占用、映射故障恢复、能力检查及 blocked 日记保留。
