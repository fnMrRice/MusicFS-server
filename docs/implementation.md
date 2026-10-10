# MusicFS Server 实施文档

状态：2026-10-10。本地扫描与文件通知、NCM、YAML配置、P5识别、P6内容与受限写回已实现；P7已有在线资料、推荐、远程同步、个人歌单及人工时间对齐恢复，P8已有安装制品、资源基线、离线备份恢复和Server生命周期协议。持久处理输出缓存及有界扫描重试也已补齐。后续已交付[Web所需查询与个人接口](web-ui-api-gaps.md)、[插件动态管理和配置](implementation/plugin-management.md)，本轮证据见[管理验收](validation/plugin-management.md)。原阶段的Windows/Linux、Edge十项及干净制品结果保留在[历史联合验收](validation/final-validation.md)；桌面Client不在本轮范围。真实平台账号、在线完整音频和未测部署环境不能据接口存在认定可用。

## 1. 范围与导航

新增 [NAS / Bilibili 插件增量计划](implementation/nas-bilibili.md)，全部待实现：原生 NFS/SMB 只读来源、B 站匿名音轨、收藏夹与音频区独立歌单，后续可选账号。此次仅设计，不扩张上文已交付范围。

当前交付范围是 Server、Server 插件和能显示文件列表并播放的 Web。桌面 Client 不在本轮实现计划内。跨端接口和联合验收见[实施总览](../../docs/implementation.md)，Server 的长期边界见[内部架构](architecture.md)与[音频设计](audio-processing.md)。

| 阶段 | 详细文档 | 核心结果 |
| --- | --- | --- |
| P0 | [工程布局与契约](implementation/00-project-layout.md) | src/app、src 各模块、tests；Go module、Schema、构建和夹具 |
| P1 | [服务基础](implementation/01-foundation.md) | 配置、启动/退出、鉴权、SQLite、持久任务 |
| P2 | [扫描与音乐库](implementation/02-library-and-scan.md) | Local Files、受限读取、稳定身份、完整扫描、修订分页 |
| P3 | [原始音频播放](implementation/03-playback.md) | 会话、固定尝试、授权流、Range、与 Web 首次闭环 |
| P4 | [音频处理](implementation/04-audio-processing.md) | 解码、PCM DSP、编码/封装、按时间定位、资源治理 |
| P5 | [资产识别](implementation/05-identification.md) | 码率、完整 SHA-512、指纹证据、人工关联和排除 |
| P6 | [刮削与写回](implementation/06-metadata.md) | 独立 Scraper、内容文件、手动/自动选用和安全写回 |
| P7 | [在线来源与歌单](implementation/07-online-library.md) | 平台适配、推荐、快照同步、隐藏、有限恢复 |
| P8 | [交付与验收](implementation/08-release.md) | 构建制品、部署、恢复、兼容矩阵与验收记录 |
| 插件专题 | [插件实现步骤](implementation/plugins.md) | 各阶段插件协议、进程、适配器、测试及参考来源 |
| 配置增量 | [YAML 配置](configuration.md) | 严格校验、显式插件/实例、修订与原子写入已实现；保存后仍需应用/重启，见[配置验收](validation/configuration.md) |
| 管理补充 | [插件管理](implementation/plugin-management.md) | HTTP 状态/配置、动态启用、配置重建与 SSE 已实现；停用保存后重启生效；管理 CLI 仍待实施 |

## 2. 实施规则与完成记录

按 `P0 → P1 → P2 → P3 → P4` 建立可播放基线，再逐项扩展。P5—P7 保留详细步骤，可以在依赖满足后并行；未验证的线上能力不影响本地文件播放。P8 中打包、恢复和真实浏览器验收适用于每次声称可用的版本，不必等待所有平台适配完成。

每个 S-Px 任务从“待实现”开始，完成时记录提交、测试命令、结果和限制。各分章保留目标；勾选仅表示所注明范围已有证据，未关闭的功能与外部环境验收分别列出。历史记录保留当时的制品数量、测试结果和限制，当前状态以本入口及后续增量记录为准。

- 一个任务完成其生产路径、失败路径和对应测试后再进入下一步；不要仅靠 mock 宣称跨进程或浏览器验证成功。
- 先定义小接口和具体数据结构，再实现需要的模块；避免为了最终目录树一次生成大量空包。
- 核心功能按配置、存储、任务、来源读取、库管理、播放等职责拆分，避免巨型 Manager、全局可写状态及无边界工具包。
- 源文件最好不超过 400 行，超出时按职责拆分。正常格式化或确实难以拆分可适当放宽，不以压缩格式凑行数；文档长度不套用源文件限制。
- 全部测试放仓库根 `tests/`。不同目录是不同 Go 包，即便 package 名相同也不能访问对方未导出成员；通过模块公开行为及依赖替身测试，不为测试随意暴露内部状态。
- 测试程序、官方插件、Server 共用一个根 go.mod。对外发布 SDK 才考虑独立 module，并增加独立兼容/CI 测试；不得把主仓库 `go test ./...` 误当自动覆盖未来子 module。

## 3. 目标技术组成

| 部分 | 初始实施选择 | 在何处验证 |
| --- | --- | --- |
| 服务语言 | Go；P0 固定受支持工具链版本 | Windows/Linux 构建、测试和运行 |
| HTTP / 日志 / JSON | 优先标准库 net/http、slog、encoding/json | P1、P3 真实请求与流测试 |
| 数据访问 | database/sql + modernc.org/sqlite，直接 SQL 与版本迁移 | P1 事务、WAL、取消、恢复；固定已验证驱动版本 |
| 插件 | 官方 Go worker，版本化 RPC；宿主管理子进程 | P2/P4/P6 的真实进程契约测试 |
| 音频工具 | 已验证构建的 ffprobe/FFmpeg，通过音频适配器调用 | P2 探测、P4 数据管线与输出矩阵 |
| Web | 独立仓库静态制品，浏览器原生 audio | [Web 实施文档](../../web/docs/implementation.md) |

这些选择已有实际实现，固定版本和构建来源见[依赖记录](dependencies.md)。不得承诺所有 FFmpeg 构建、codec、平台或 SQLite 驱动版本均可用。若某依赖不满足阶段验收，先记录具体失败和替代影响，再调整实现文档。

## 4. 两个必须交付的播放节点

**P3 原流节点：** 从全新数据目录启动、配置一个只读音乐根、扫描、打开 Web、列出文件、点击播放、暂停、定位、切歌、停止、重启后再播放；至少一个明示支持的输入格式在真实浏览器实际解码。Edge非零PCM与操作序列已验收，人工扬声器听音仍单列待验。

**P4 处理节点：** 选择明确不走直放的测试输入，经解复用/解码、可观察的增益 DSP、编码/封装形成浏览器可播输出；流未全部生成即可实际解码，非零曲目时间定位生成新尝试并正确映射，取消能清理进程。已有真实浏览器PCM/频率标记证据，不能将其替代人工听音或其他浏览器验收。

每个节点同时保留已知限制和演示步骤，样本不能依赖私人平台账号。源资产未知哈希、未关联 Recording 或没有歌词，不得阻止本地基础播放。

## 5. 文档与参考入口

平台开源参考清单在 [README](../README.md)，插件文档说明如何把外部接口映射为 MusicFS 能力，而不将平台返回直接暴露为核心数据模型。关键工程依据为 [Go module](https://go.dev/doc/modules/layout)、[testing](https://pkg.go.dev/testing)与[SQLite 驱动](https://pkg.go.dev/modernc.org/sqlite)的一手文档。
