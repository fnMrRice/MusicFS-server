# S-P6-05 配置、接口与真实服务写回验收

日期：2026-10-10。当前使用和恢复语义见[写回说明](../writeback.md)，逐文件保护与故障注入见[独立模块记录](writeback-module.md)。本页不将模块测试替代服务组装证据。

## 工具和样本

Windows amd64，Go1.26.9，使用当前 go.mod：yaml3.0.5、x/sys0.49.0、x/text0.43.0、SQLite1.60.1、jsonschema6.0.3。没有修改依赖或新增Node运行依赖。实际FFmpeg/ffprobe为既有6.1.1构建。

真实服务以临时YAML启动，构建独立Local Files与音频Worker。仅把仓库CC0 `lossless.flac` 复制到测试目录，并生成文本歌词和PNG；不读取或改写私人音乐。设置两个授权读取根，但只允许其中一个写回，以检验显式根授权。所有媒体、数据库、日志和临时文件均在隔离目录。

## 定向检查

| 范围 | 实际结果 |
| --- | --- |
| 完整配置测试：默认关闭、严格YAML/Schema、上下限、未知字段、显式根与字段、双层启用依赖、Snapshot/Current深拷贝 | `go test -mod=readonly ./tests/unit/config -count=1`：2.212s，通过 |
| 真实SQLite+内容文件+HTTP：显式六字段、路径/正文注入拒绝、Cookie/Origin/CSRF、归属隔离、创建重放、取消及恢复状态边界；全部Schema/样例/OpenAPI | 定向3.900s，通过 |
| 配置和HTTP上述定向竞争检查 | 配置7.059s、HTTP22.942s，通过 |
| 真实服务 `TestWriteBackActualServerPlaybackGateSelectionAndRestart` | 7.10s，包7.801s，通过 |
| 同一真实服务竞争检查 | `go test -mod=readonly -race ./tests/e2e -run TestWriteBackActual -count=1 -v`：9.62s，包11.731s，通过 |

真实服务验证包括：

- 默认关闭：已有人工选择也不能写回，原文件逐字节不变；Provider没有隐式开启根权限。
- 明确开启后，真实NTFS能力为available；同实例另一个未授权根403，原件最终保持不变。过期资产、过期选择修订和未授权覆盖均拒绝。
- 使用现有FLAC→MP3处理播放，交付结果实际解码为非零PCM。播放会话仍活动时，写回经过多个后台轮询周期保持queued，原件不变；显式Stop后才执行。
- 在占用期间取消另一queued任务，不借取消改写文件。所有任务响应经过公开Schema校验。
- 选用的歌词真实写入FLAC；写前/写后原文件解码PCM逐字节相同，文件完整SHA-512改变并与任务结果一致。数据库新资产以writeback-full-file-v1专属证据保存verified摘要。
- 同一SourceTrack建立新资产，旧资产/版本不再冒充当前；歌词与未参与本次写回的封面同时保留原ObjectID、人工锁、available/current，选择修订只增加一次。
- 源已改变后，原键仍重放原Job；已完整完成任务的recover返回同一任务。写回后新播放继续获得可解码的MP3。
- 关闭写回并重启、重新扫描后，SourceTrackID、新资产/版本/摘要和人工选择保持，历史Job可读，新提交403。

## 首次失败与修正

HTTP文件发布首次在受限沙箱出现Access denied；允许隔离测试目录的原子发布后通过，没有放宽生产权限。真实服务首次运行也不是一次成功：目录能力误要求FILE_DELETE_CHILD，不能正确接受具有Modify权限的根；修正为创建权限与逐文件DELETE保护分别检查。随后FileStreamInfo查询暴露缓冲区8字节对齐要求，改用对齐存储并保留ADS拒绝检查。上述7.10s为两项修复后的完整重跑。

测试最初请求FLAC直出，被现有播放策略正确返回415；测试改用既有MP3处理链，没有为写回扩展播放格式。恢复逻辑合并期间一次编译检查缺runRecovery，补齐后才继续执行；不将该轮计为通过。

## 范围

恢复请求200只表示返回当前状态；后台重试完成以GET为准。真实崩溃点、外部修改、磁盘/权限失败和日记重放由独立模块及宿主恢复测试记录，不以本页正常流程覆盖代替。

本页证明本机Windows固定本地NTFS和上述合成FLAC；不证明NAS、其他文件系统、Linux写回、macOS/ARM、所有标签变体或任意断电安全。Linux服务应明确返回UNSUPPORTED。未运行新浏览器流程、未新增Web功能，也未声称人工听音；音频证据是实际解码和字节比较。最终全量及双平台结果由冻结快照验收补录。
