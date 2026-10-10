# 本地最简 Web 运行

最简Web提供本地文件列表、授权原流、Go DSP处理播放和本地NCM解密。Server另提供指纹/人工关联、歌词/封面、专辑、[公开在线资料](online.md)、[推荐](recommendations.md)、[远程歌单](remote-playlists.md)和[个人歌单](playlists.md)API；这些功能没有新增Web管理页面。默认关闭的原件写回见[写回说明](writeback.md)。桌面Client、平台账号登录与在线完整音频未实现；实际范围见[实施记录](validation/progress.md)及[Web兼容记录](../../web/docs/compatibility.md)。

## Windows

前提：Go 1.26.9，Web 使用其 package.json 固定工具链；FFmpeg/ffprobe 6.1.1 是本轮实测构建，须包含 libmp3lame。FFmpeg 不随仓库分发。先在 Web 仓库运行 `npm ci`、`npm run build`。

在 Server 仓库运行：

```powershell
./scripts/build.ps1
Copy-Item configs/server.example.yaml configs/server.local.yaml
```

构建生成 `musicfs.exe` 与 `musicfs-plugins.exe` 两个程序；内置角色由配置中的 `args` 选择，仍分别启动进程。编辑 `configs/server.local.yaml`（旧JSON配置不兼容，字段及读写边界见[YAML配置](configuration.md)）：

- `plugins` 的 `musicfs.localfiles` 实例默认没有选定的导入目录，采用[默认目录授权](plugin-configuration.md#部署授权)。需要指定其他允许范围时，在 YAML 中设置 `permissions.readDirectories`。启动后用管理员账号打开 Web「设置 → 音乐来源」，点击「本地音乐」标题右侧的「管理目录」。弹窗中的目录树以允许访问的目录为根，展开并选择根或子目录；下方配置递归查找，按需勾选深度限制后保存。默认不限制递归深度；没有已选目录时标题下显示「暂无已选择目录」。之后通过扫描接口或脚本手动导入。实际选择与递归设置保存在插件配置数据库，YAML 不因选择而改写；旧 `config.musicRoots` 仍兼容。
- `musicfs.audio.ffmpeg` 实例的 `config.ffmpegPath`、`config.ffprobePath` 指向本机工具；配置齐全且未显式禁用时自动启动。缺少路径时显示待配置，仅验证标准PCM16 WAV原流时不需要启用；AIFF与当前NCM FLAC转码路径需要两种工具。
- 内置插件的 `executable` 指向统一程序，`args` 选择角色；示例默认 Windows 构建路径，省略 enabled 时自动启用。把 `musicfs.ncm.enabled` 设为false并重启可停用本地NCM解密，其他格式仍可使用。
- 各插件可配置 `sha256` 固定管理员审核过的二进制摘要（`Get-FileHash -Algorithm SHA256`）。省略时信任显式配置的本机可执行路径，只核对本次启动的读取一致性，不承诺跨重启钉住版本；更新制品后需同步显式摘要。
- 音乐目录与 `dataRoot/mediaRoot/cacheRoot` 保持分离。相对授权路径以 YAML 所在目录为基准；修改 YAML 权限需重启。Web 保存音乐目录通过管理接口应用，无需重启 Server；尚未选择目录时显示「待配置」。
- `allowedOrigins` 必须与页面实际 URL 完全一致。默认只允许明确配置的 loopback HTTP，远程监听必须配置 TLS。

```powershell
$env:MUSICFS_ADMIN_TOKEN = (& ./build/windows/musicfs.exe token).Trim()
./build/windows/musicfs.exe serve --config ./configs/server.local.yaml --web-root (Resolve-Path ../web/dist).Path
```

终端会阻塞运行服务。凭据保存在本次终端环境中，需要预先复制给登录输入框；`musicfs token` 是显式管理输出，普通服务日志不会打印凭据。不要将真实凭据提交到配置或复制到 URL。

扫描由管理 API 执行，页面不承担目录管理。在另一个设置了同一 `MUSICFS_ADMIN_TOKEN` 的终端运行 `./scripts/scan.ps1 -ProviderID local`；脚本使用独立临时登录会话，先读取 Provider 声明和授权根列表，再提交 `mode/schemaRevision/rootIds/options`，等待扫描完成并退出该会话。默认选择该实例全部授权根并使用实例导入默认值。Linux 可用 PowerShell 7 执行脚本，或按 OpenAPI 使用同样的登录、创建扫描和查询任务接口；旧的仅 `{mode}` 请求不再接受。

只扫描选中根的本层文件可运行 `./scripts/scan.ps1 -ProviderID local -RootIDs local -Recursive:$false`；扫描到两层子目录可运行 `./scripts/scan.ps1 -ProviderID local -RootIDs local -Recursive:$true -MaxDepth 2`；扫描全部子目录使用 `-Recursive:$true -MaxDepth 0`。多个根使用 `-RootIDs root1,root2`，增量模式使用 `-Mode incremental`。根编号应来自 Provider 的 `roots`，不是磁盘路径；启用本地 Scraper 后，`-ImportLyrics:$true` 会在扫描成功后独立补齐歌词；扫描完成后继续查询 `/api/v1/scan-jobs/{id}/metadata-results` 确认逐项结果。递归开关与深度的逐层规则见[配置说明](configuration.md#导入选项)。脚本最多等待一小时；超时后先用返回的任务状态查询接口确认结果，不假定任务已取消。

排除临时目录可传 `-ExcludePatterns '**/cache','**/*.tmp'`，`-ExcludePatterns @()`清除实例继承。需要自动复核时，在Local实例的`config.watch`中显式启用后重启；默认仍手动扫描。监听与周期复核使用实例默认范围，手工覆盖不改变监听配置。`retry_wait`继续等待原任务；`paused`需修正依赖/权限后手工重试，自动监听须重启才恢复。具体范围、资源上限及跨范围移动的复核方法见[扫描说明](scanning.md)。

打开 `http://127.0.0.1:8080`，使用管理员凭据登录，选择来源并刷新列表。点击真实文件的播放按钮，使用原生控件暂停/继续；原流时间轴可定位。选中服务端处理选项后可使用简单曲目位置输入，处理后原生时间轴只代表当前片段。按 Ctrl+C 退出。

测试素材在 `tests/testdata/audio`；包含项目自行生成的 CC0 文件、AIFF、VBR、中文名以及损坏样本。不要将损坏样本当作可播放成功标准。

NCM无需新增页面操作：配置Worker、重新扫描后直接选曲。内部MP3以固定解密表示交付，内部FLAC按当前策略进入MP3处理链；后者需要FFmpeg/ffprobe。仅配置NCM Worker时可播放兼容的内部MP3，未探测的时长/码率保持未知。合成样本真实浏览器证据见[NCM说明](ncm.md)，后续真实本地库4个NCM的HTTP播放、定位和原件不变证据见[真实库验收](validation/real-local-library.md)；未外推所有客户端格式变体。

## Linux 与同源部署

使用 `sh scripts/build.sh` 构建相同两个程序（无 `.exe`），复制相同YAML示例，并将 `pluginBundle` 与各 `plugins[].executable` 改为 `../build/linux/musicfs-plugins`，保留 args，配置音频实例的 FFmpeg/ffprobe 路径后自动启用（显式禁用除外）。服务仍可通过 `--web-root` 提供独立 Web 制品。NCM 真实 Worker 已在 Arch WSL2 通过；Linux 配置与运行验收见[配置记录](validation/configuration.md)，与交叉构建结果分开，不由构建推断所有发行版支持。

2026-10-10已在Arch WSL使用原生FFmpeg/ffprobe 6.1.6通过YAML处理后播放、实际PCM定位、音频启用边界和同配置重启验证；工具为官方签名源码的LGPLv2.1+本地构建，不随仓库分发。工具能力、定向结果与范围见[Linux音频记录](validation/configuration-linux-audio.md)。这项Server验证不代替Linux浏览器、人工听音或其他架构/系统验收，也不包含动态启停。

开发期Vite仅代理 `/api` 到Server；静态页面和API必须同源。远程访问使用 `tlsCert/tlsKey` 与HTTPS Origin；不信任代理转发头。直接TLS已有双平台实际请求验证；安装制品、Docker/反向代理示例及其未验边界见[部署说明](deployment.md)。Server已提供[本地生命周期租约](lifecycle.md)，默认仍独立启动；桌面Client尚未实现。

## 数据与恢复

SQLite WAL 只放本地文件系统；音乐可以来自已挂载目录。初次成功注册记录根身份，身份变化或无法打开时保留旧库并将来源视为离线。恢复原挂载后重启服务；不要通过删除状态文件来规避身份检查。

正常停止服务后，用 `musicfs backup --config <配置> --output <已存在的备份父目录>` 创建完整包，再用 `verify-backup --input <备份目录>` 校验；`restore --input <备份目录> --destination <不存在的新安装目录>` 恢复。备份含一致性SQLite、被引用媒体、插件身份/恢复状态与配置，缓存可重建。原音乐、原件旁边的写回恢复文件、令牌、TLS私钥和程序须另外保留。恢复默认撤销旧会话并禁用插件/自动任务/写回，重新配置后再扫描播放。精确限制及回滚流程见[完整备份恢复](backup.md)，不要仅复制活动WAL主数据库。

## 当前资源和能力边界

- 默认最多8个播放会话、2条同时处理管线；`maxCacheBytes`默认1GiB，由输入快照、处理临时输出、完整缓存和待删除文件共同计费。输入复制超预算明确拒绝，输出缓存不能写入时仍可交付未缓存处理流。
- 每个来源最多50,000个候选；增量请求仍执行完整受控复核。可选文件通知只触发任务，周期完整复核补漏，不将通知当作完整文件清单。
- 原流以私有不可变磁盘副本提供真实 Range；输入复制为有界流，不在内存缓存整首歌。复制准备期间尚未开始播放。
- NCM 原文件保持只读，解密表示也受同一磁盘预算约束。解密 Worker 默认并发 2、打开限 5 秒、任务最长 2 分钟；复制结束即回收，随后从固定快照播放。
- 处理输出为 MP3 128 kb/s、48 kHz、双声道；Go PCM 增益在 -60 到 0 dB，未经处理的原始文件不被修改。
- 完整处理输出可按来源/用户/资产/工具/参数/起点私有复用，只有成功完成才发布；缓存命中仍不声明长度/ETag/字节定位，定位仍使用timeRestart。配额、半成品清理和硬期限见[完整处理输出缓存](processed-cache.md)。
- 停止、退出撤销资源；无消费者会话有 90 秒空闲宽限期，最长 30 分钟且不超过登录会话有效期。超过期限后需要重新播放。
- 浏览器中间断线可能需要明确重新播放；Server同源及跨来源恢复有次数、目标数与总期限。跨来源只有适用的人工时间对齐证据才可继续，默认提示重新播放，见[恢复说明](recovery.md)；最简Web没有该管理界面。
- Worker 是受信任官方程序，当前不是恶意插件的完整操作系统沙箱。

本地歌词/封面补全使用独立 `musicfs.scraper.local` 实例。示例已双层启用该插件，构建包含对应程序；配置、逐目标预览与人工选用见[补全任务](metadata.md)，无需启用FFmpeg或指纹。修改YAML后重启生效，不提供动态启停；单次 `importLyrics` 与默认关闭的 `metadata.automatic` 可分别开启，人工锁不被覆盖；详见[自动补缺](metadata.md#导入歌词与自动补缺)。
