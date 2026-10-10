# S-P8-07：实施清单与当前文档核对

核对日期：2026-10-10。本次核对Server的生产入口、00—08实施章节、公共契约、测试与既有验证记录；本项仅作静态核对，不将历史测试次数改成当前回归结果。每步提交由[实施记录](progress.md)维护；另行完成的本轮完整检查、Edge十项回归及干净制品结果集中在[最终联合验收](final-validation.md)。表中“已实现”仅限写明的功能与环境；未测环境保留原计划门槛，没有删除。

本次原阶段核对时构建八个程序，后续分发变化以[配置说明](../configuration.md)为准。`src/app`只有薄入口，实际组装在 `src/bootstrap`。旧JSON配置和未上线开发契约不作兼容保留。后续授权已实现动态插件启停及实例配置在线应用，见[增量验收](plugin-management.md)；原阶段表格不代替新增功能验证。

## P0：工程与契约

| 步骤 | 核对结果与证据 |
| --- | --- |
| S-P0-01 | 已固定Go/module/依赖与忽略规则；[依赖](../dependencies.md)、[双平台制品](release.md)。交叉构建和Linux原生运行分开记录 |
| S-P0-02 | 已按API→服务→持久/来源接口注入，bootstrap组装；[包约定](packages.md)。最初 `src/stream` 职责由playback/API实现，无需空包 |
| S-P0-03 | 根tests分层与独立testkit已使用；生产不导入tests，浏览器测试归独立Web仓库 |
| S-P0-04 | Windows/Unix build/check脚本、gofmt/vet/覆盖率/race入口已实现；实际全量结果见[进度](progress.md) |
| S-P0-05 | OpenAPI/HTTP与分角色插件Schema、真实DTO及正反样本已实现；未支持能力明确拒绝，协议主版本拒绝不等于旧开发契约兼容 |
| S-P0-06 | 可再生成CC0音频、标记/损坏/空文件/VBR/中文名夹具已有[来源记录](fixtures.md)；人工听音未完成 |

## P1：服务基础

| 步骤 | 核对结果与证据 |
| --- | --- |
| S-P1-01 | 严格YAML、配置目录路径基准、运行目录分离、双层显式开关、脱敏及读写修订已实现；[配置验收](configuration.md) |
| S-P1-02 | 启动锁、ready、取消与有界收尾已实现；后续[生命周期](lifecycle.md)验证清理超时仍持锁，未把桌面Client算作完成 |
| S-P1-03 | 控制/上传/媒体流/音频流分别有界；统一错误、Origin/CSRF和安全字段有契约；[媒体HTTP](media-http.md)、[直接TLS](release.md) |
| S-P1-04 | 管理令牌交换Cookie、会话摘要/撤销/CSRF/限流及流授权已实现；[历史基线](progress.md)。没有完整多用户管理界面 |
| S-P1-05 | WAL、事务迁移、升级前一致性备份、结构版本拒绝和重启已验证；完整安装备份见[备份记录](backup.md)，不承诺任意旧二进制降级 |
| S-P1-06 | 持久扫描Scheduler支持最多3次执行、退避/抖动、retry_wait合并、暂停及重启预算，见[重试验收](scan-retries.md)；其他业务沿自身恢复规则，不盲目重放原件副作用 |

## P2：扫描与音乐库

| 步骤 | 核对结果与证据 |
| --- | --- |
| S-P2-01 | 正式Local Worker握手、配置声明、故障/取消与启动开关已实现；[插件](plugins.md)、[导入选项](plugin-import-options.md)。动态平滑/强制停用后置 |
| S-P2-02 | 授权根、资源引用、受限输入、版本/身份检查与任务通道已实现；[资产证据](identification.md)、[写回保护](writeback.md)。官方Worker是受信任程序，不是OS沙箱 |
| S-P2-03 | 完整计划、选根/递归/深度、稳定别名/硬链接、scope内缺失发布及旧代次拒绝；另补[排除规则与旧身份保护](scan-exclusions.md)、[通知合并和周期复核](scan-watch.md)。[真实双根](real-local-library.md)、[快照期限](scan-snapshot-lifetime.md)保留原证据 |
| S-P2-04 | 受控ffprobe、失败unknown、来源证据与asset换版已实现；[P5证据](identification.md)。探测不猜CBR/VBR，保留unknown |
| S-P2-05 | 当前owner的Provider安全说明、固定库修订分页/期限/权限已实现；[导入HTTP](plugin-import-options.md)、[Web兼容](../../../web/docs/compatibility.md) |

实体NAS失挂/重挂、慢网络卷与完整文件系统矩阵仍待外部环境验收；本地根身份替换/离线模拟不能替代它们。

## P3：原流与Web

| 步骤 | 核对结果与证据 |
| --- | --- |
| S-P3-01 | sourceTrack授权/可用性与原流/处理选择已实现，未知摘要/关联/歌词不阻止播放；[运行](../running.md) |
| S-P3-02 | session/attempt/representation分离、固定快照、持久幂等及期限/配额已验证；[基线](progress.md) |
| S-P3-03 | 精确登录会话、独立读取偏移、取消和不可变输入已验证；客户端无源路径 |
| S-P3-04 | GET/HEAD、单Range、206/416、ETag/If-Range及逐字节检查通过；[真实库](real-local-library.md)、[安装包](release.md) |
| S-P3-05 | 串行attempt变更、同键重放、停止/撤销及有限同源恢复已实现；后续[跨源恢复](../recovery.md)使用独立人工证据 |
| S-P3-06 | 真实Edge登录/列表/暂停/定位/切歌/停止/注销与非零PCM通过；[Web记录](../../../web/docs/compatibility.md)。人工扬声器听音保留待验 |

## P4：音频处理

| 步骤 | 核对结果与证据 |
| --- | --- |
| S-P4-01 | FFmpeg/ffprobe版本与能力实探、受限demuxer及不支持拒绝已实现；[插件验证](plugins.md)、[Linux音频](configuration-linux-audio.md) |
| S-P4-02 | 任务限定二进制通道和独立音频Worker流式输出已实现；长音频在处理完成前实际解码，未以整曲内存Blob替代 |
| S-P4-03 | PCM格式/块边界、固定参数/profile/版本与effectiveProcessing已实现；大块音频不进入JSON |
| S-P4-04 | 独立Go gain节点实际接在decoder/encoder之间；RMS比例0.5和原件不变验证，不是仅FFmpeg滤镜配置 |
| S-P4-05 | timeRestart新attempt/URL、曲目起点映射、控制串行/幂等和取消已验证 |
| S-P4-06 | Windows/WSL真实PCM标记与约23ms起音，当前MP3容差100ms；Edge快速定位已验，不承诺采样级无缝 |
| S-P4-07 | 有界准入/背压/期限与进程树取消已验证；Windows四轮并行后台任务与慢消费者见[资源基线](resource-baseline.md)，非操作系统CPU硬隔离 |
| S-P4-08 | [完整输出缓存](../processed-cache.md)已通过Windows/Linux真实Worker/播放race：私有身份、完整发布、授权复查、跨会话/重启复用、共享配额、引用回收；HTTP仍为processed/timeRestart |

## P5：资产识别

| 步骤 | 核对结果与证据 |
| --- | --- |
| S-P5-01 | 申报/探测/估算/人工证据、代次、失效与历史分离；[识别验收](identification.md) |
| S-P5-02 | 稳定整文件SHA-512、默认关闭自动计算、持久队列/速率/冲突/取消/恢复；[摘要HTTP](hashing-api.md)、[真实库](real-local-library.md) |
| S-P5-03 | 固定fpcalc 1.6.1、DSP前局部规范化、明细文件/恢复和API；[指纹验收](fingerprinting.md)。不上传外部服务 |
| S-P5-04 | 分层候选、人工link/unlink/split/exclusion、expectedRevision/幂等与可逆审计；[关联验收](matching.md)，不自动生成时间证据 |

## P6：内容、刮削与写回

| 步骤 | 核对结果与证据 |
| --- | --- |
| S-P6-01 | 上传校验/预览/人工锁与版本、来源专辑/发行独立封面、有界缩略图/完整刮削文档；[媒体](media.md)、[专辑与派生](albums-media.md) |
| S-P6-02 | 持久发布、崩溃恢复、引用保护回收及一致性媒体备份；[存储验收](media-storage.md)。完整安装备份属P8 |
| S-P6-03 | 独立本地Scraper、内嵌/旁置、多目标/专辑固定快照、候选与逐字段人工选用；[Scraper验收](metadata.md)。本地查询修正明确UNSUPPORTED，无远程Scraper |
| S-P6-04 | 歌词导入、默认关闭自动补缺、优先级/语言/依据/人工锁；[自动补缺](metadata-automatic.md)保存当时v2证据，当前v3声明另含排除规则。不是仅提供空开关 |
| S-P6-05 | 显式根/字段授权、活动读取协调、NTFS FLAC可恢复替换、新asset/hash及选用锁迁移；[写回验收](writeback.md)。Linux/NAS不支持原件写回 |

## P7：在线资料与歌单

| 步骤 | 核对结果与证据 |
| --- | --- |
| S-P7-01 | 固定参考提交/许可、官方资料、匿名探测分列；[调查](platform-research.md)，原结果保留为历史，不代替后续能力 |
| S-P7-02 | 三个正式Go Worker、作用域/受控网络、YAML/HTTP接入；[宿主验收](online-http.md)。网易四项公开资料通过，QQ近期失败、Apple缺授权；账号和裸流明确不支持 |
| S-P7-03 | 有界匿名推荐聚合、UTC/缓存隔离、冷却、部分失败和stale；[推荐HTTP](recommendations-http.md)。没有个性化账号实测 |
| S-P7-04 | 显式绑定/同步、重复出现稳定出处、完整观测确认移除、持久代次/取消/历史页；[远程歌单](remote-playlists-http.md) |
| S-P7-05 | 有序聚合/循环拒绝、全部出处、独立隐藏修订、关联扩大需确认、固定快照；[个人歌单](playlists-http.md)。未写回平台 |
| S-P7-06 | 有界同源/跨源恢复、人工方向性offset、资产/Recording/profile双端复核；[真实音频与权限](../recovery.md)。默认promptRestart，不生成假时间证据 |

P7允许逐平台能力交付，因此元数据可见不等于歌曲可播放。账号登录/刷新/私人歌单/个性化推荐及在线音频仍未实现，需要后续真实授权验收；匿名夹具只证明本地隔离规则。

## P8：交付与验收

| 步骤 | 核对结果与证据 |
| --- | --- |
| S-P8-01 | Windows/Unix检查入口、分层测试、覆盖率与race已用；最终冻结回归由[进度](progress.md)记录，不把本次文档检查当新回归 |
| S-P8-02 | 所测Windows/WSL、Edge/profile及真实双根库已有基线；[Windows资源测量](resource-baseline.md)记录四轮并行hash/指纹/慢消费者、CPU/内存/句柄/TCP与缓存，不外推长期运行、NAS或Linux同负载 |
| S-P8-03 | 八程序包、静态Web可选、配置、许可/依赖与SHA256清单、新目录实跑；[制品验证](release.md)。开发验证包明确dirty，正式干净制品另行生成 |
| S-P8-04 | 本地/TLS实跑及Docker/Compose/Nginx样例已提供；[部署](../deployment.md)。无Docker引擎运行、Nginx代理或远程浏览器实测 |
| S-P8-05 | 离线完整包/校验/新目录恢复、会话撤销/默认禁用、历史媒体与原件不变；[备份](backup.md)。同OS/架构/schema限定，不支持随意降级 |
| S-P8-06 | Server启动归属、loopback鉴权租约、续租序号/释放/崩溃/多客户端和有界退出；[生命周期](lifecycle.md)。没有桌面Client实现 |
| S-P8-07 | 本文完成53步文档与清单核对；核对发现的重试/排除/通知/输出缓存缺口已分别实施验证，冻结后的完整检查与干净制品见[最终联合验收](final-validation.md)，外部环境仍列下方待验 |

## 未关闭项目的分类

**本轮发现并补齐的功能：** 扫描临时错误退避/抖动、可配置排除与旧身份范围防护、文件通知/周期补漏及P4-08持久输出缓存均已有生产路径与定向验证，未从原计划删除。最终冻结联合检查和干净制品仍以主实施记录为准。

**明确后置或能力边界：** 动态插件启停已另行实现，见[管理验收](plugin-management.md)；Web本轮仅接入SSE能力变更和已有来源列表刷新。在线账号、完整音频与远程Scraper仍为未实现/未验证能力，不因匿名子集完成而承诺。最简Web不增加这些管理页面，桌面Client未实施。

**外部环境与人工验收：** 实体NAS失挂/重挂和慢存储；ARM64、macOS及其他发行版；Linux/其他浏览器；人工扬声器听音；Docker实际运行、Nginx/TLS代理与远程浏览器联调；另一台干净物理机器安装。当前Windows/WSL新目录与实际CLI验证不能替代这些项目。

**文档检查：** 清理了“当前四程序”“真实NCM从未验收”“在线/歌单尚未实现”“没有Server租约/容器方案”等过期概括；保留首次阶段的四程序、合成样本、旧歌词拒绝断言与测量值，并加后续记录链接。没有将未验证能力改写为全勾选，没有更新主代理持有的 `progress.md`。

静态检查覆盖本次18份文档的279个相对链接及章节锚点，00—08共53个步骤与本文53行一一对应，无遗漏或重复；最终联合验收记录也已补齐，差异格式检查通过。此检查不重新验证外部网站，也不代替功能测试。
