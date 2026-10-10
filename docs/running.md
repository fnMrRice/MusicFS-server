# 本地最简 Web 运行

此版本实现本地文件扫描、固定修订列表、授权原流、独立 Go DSP 处理播放和本地 NCM 解密。桌面 Client、在线 Provider、刮削、指纹与歌单尚未实现。实际验证范围见 [实施记录](validation/progress.md)及 Web 的兼容性记录。

## Windows

前提：Go 1.26.9，Web 使用其 package.json 固定工具链；FFmpeg/ffprobe 6.1.1 是本轮实测构建，须包含 libmp3lame。FFmpeg 不随仓库分发。先在 Web 仓库运行 `npm ci`、`npm run build`。

在 Server 仓库运行：

```powershell
./scripts/build.ps1
Copy-Item configs/server.example.yaml configs/server.local.yaml
```

构建生成 `musicfs.exe`、`musicfs-plugin-local.exe`、`musicfs-plugin-audio-ffmpeg.exe`、`musicfs-plugin-ncm.exe`、`musicfs-plugin-scraper-local.exe` 五个程序。编辑 `configs/server.local.yaml`（旧JSON配置不兼容，字段及读写边界见[YAML配置](configuration.md)）：

- `plugins` 的 `musicfs.localfiles` 实例在 `config.musicRoots` 下配置音乐根的 `id/name/path`；示例使用项目合成样本，可替换为自己的音乐目录。保持已使用的实例ID和根ID稳定。
- `musicfs.audio.ffmpeg` 实例的 `config.ffmpegPath`、`config.ffprobePath` 指向本机工具；需要转码/DSP时将该插件和实例的 `enabled` 都改为true。示例默认关闭音频处理，仅验证标准PCM16 WAV原流时不需要启用；AIFF与当前NCM FLAC转码路径需要两种工具。
- 各插件的 `executable` 指向对应程序；示例默认Windows构建路径，Local Files与NCM的插件/实例显式启用。把 `musicfs.ncm.enabled` 设为false并重启可停用本地NCM解密，其他格式仍可使用。
- 各插件可配置 `sha256` 固定管理员审核过的二进制摘要（`Get-FileHash -Algorithm SHA256`）。省略时信任显式配置的本机可执行路径，只核对本次启动的读取一致性，不承诺跨重启钉住版本；更新制品后需同步显式摘要。
- 音乐目录与 `dataRoot/mediaRoot/cacheRoot` 保持分离。相对配置路径以 YAML 所在目录为基准，修改后重启应用；当前没有自动热重载或动态启停管理入口。
- `allowedOrigins` 必须与页面实际 URL 完全一致。默认只允许明确配置的 loopback HTTP，远程监听必须配置 TLS。

```powershell
$env:MUSICFS_ADMIN_TOKEN = (& ./build/windows/musicfs.exe token).Trim()
./build/windows/musicfs.exe serve --config ./configs/server.local.yaml --web-root (Resolve-Path ../web/dist).Path
```

终端会阻塞运行服务。凭据保存在本次终端环境中，需要预先复制给登录输入框；`musicfs token` 是显式管理输出，普通服务日志不会打印凭据。不要将真实凭据提交到配置或复制到 URL。

扫描由管理 API 执行，页面不承担目录管理。在另一个设置了同一 `MUSICFS_ADMIN_TOKEN` 的终端运行 `./scripts/scan.ps1 -ProviderID local`；脚本使用独立临时登录会话，先读取 Provider 声明和授权根列表，再提交 `mode/schemaRevision/rootIds/options`，等待扫描完成并退出该会话。默认选择该实例全部授权根并使用实例导入默认值。Linux 可用 PowerShell 7 执行脚本，或按 OpenAPI 使用同样的登录、创建扫描和查询任务接口；旧的仅 `{mode}` 请求不再接受。

只扫描选中根的本层文件可运行 `./scripts/scan.ps1 -ProviderID local -RootIDs local -Recursive:$false`；扫描到两层子目录可运行 `./scripts/scan.ps1 -ProviderID local -RootIDs local -Recursive:$true -MaxDepth 2`。多个根使用 `-RootIDs root1,root2`，增量模式使用 `-Mode incremental`。根编号应来自 Provider 的 `roots`，不是磁盘路径；`importLyrics` 尚未实现，`-ImportLyrics:$true` 会明确失败。递归开关与深度的逐层规则见[配置说明](configuration.md#导入选项)。脚本最多等待一小时；超时后先用返回的任务状态查询接口确认结果，不假定任务已取消。

打开 `http://127.0.0.1:8080`，使用管理员凭据登录，选择来源并刷新列表。点击真实文件的播放按钮，使用原生控件暂停/继续；原流时间轴可定位。选中服务端处理选项后可使用简单曲目位置输入，处理后原生时间轴只代表当前片段。按 Ctrl+C 退出。

测试素材在 `tests/testdata/audio`；包含项目自行生成的 CC0 文件、AIFF、VBR、中文名以及损坏样本。不要将损坏样本当作可播放成功标准。

NCM 无需新增页面操作：配置 Worker、重新扫描后直接选曲。内部 MP3 以固定解密表示交付，内部 FLAC 按当前策略进入 MP3 处理链；后者需要 FFmpeg/ffprobe。仅配置 NCM Worker 时可播放兼容的内部 MP3，未探测的时长/码率保持未知。手工检查及真实浏览器三项证据见 [NCM 插件说明](ncm.md)；真实下载 NCM 样本尚未验收。

## Linux 与同源部署

使用 `sh scripts/build.sh` 构建相同五个程序（无 `.exe`），复制相同YAML示例，并将各 `plugins[].executable` 改为 `../build/linux/` 下对应程序，配置音频实例的FFmpeg/ffprobe路径后显式启用。服务仍可通过 `--web-root` 提供独立 Web 制品。NCM 真实 Worker 已在 Arch WSL2 通过；Linux 配置与运行验收见[配置记录](validation/configuration.md)，与交叉构建结果分开，不由构建推断所有发行版支持。

2026-10-10已在Arch WSL使用原生FFmpeg/ffprobe 6.1.6通过YAML处理后播放、实际PCM定位、音频启用边界和同配置重启验证；工具为官方签名源码的LGPLv2.1+本地构建，不随仓库分发。工具能力、定向结果与范围见[Linux音频记录](validation/configuration-linux-audio.md)。这项Server验证不代替Linux浏览器、人工听音或其他架构/系统验收，也不包含动态启停。

开发期 Vite 仅代理 `/api` 到 Server；交付静态页面和 API 必须同源。远程访问使用 Server 的 `tlsCert/tlsKey` 与 HTTPS Origin；不会信任未配置的代理转发头。当前不提供桌面管理租约或容器运维方案。

## 数据与恢复

SQLite WAL 只放本地文件系统；音乐可以来自已挂载目录。初次成功注册记录根身份，身份变化或无法打开时保留旧库并将来源视为离线。恢复原挂载后重启服务；不要通过删除状态文件来规避身份检查。

备份前正常停止服务，保留完整 `dataRoot`（含数据库、插件稳定身份与根身份）、`mediaRoot`、配置和所用制品版本。凭据单独安全保存。`cacheRoot` 可重建，无需备份。恢复到新的隔离目录后调整配置中的路径，重新登录并扫描验证。库内 API 使用 SQLite `VACUUM INTO` 生成一致性数据库备份，自动测试已覆盖从该备份重新打开。跨版本回滚应恢复相应备份，不能任意使用旧二进制打开新 schema。

## 当前资源和能力边界

- 默认最多 8 个播放会话、2 条同时处理管线，私有原文件快照总额 1 GiB；复制超出预算会明确拒绝。
- 每个来源最多 50,000 个候选；增量请求当前执行完整受控复核，不依赖文件通知。
- 原流以私有不可变磁盘副本提供真实 Range；输入复制为有界流，不在内存缓存整首歌。复制准备期间尚未开始播放。
- NCM 原文件保持只读，解密表示也受同一磁盘预算约束。解密 Worker 默认并发 2、打开限 5 秒、任务最长 2 分钟；复制结束即回收，随后从固定快照播放。
- 处理输出为 MP3 128 kb/s、48 kHz、双声道；Go PCM 增益在 -60 到 0 dB，未经处理的原始文件不被修改。
- 处理输出不持久缓存，不声明原始长度/ETag/字节定位；固定输入、参数和起点的处理尝试通过时间定位重建。
- 停止、退出撤销资源；无消费者会话有 90 秒空闲宽限期，最长 30 分钟且不超过登录会话有效期。超过期限后需要重新播放。
- 浏览器中间断线可能需要明确重新播放。恢复次数有上限；没有实现跨来源自动续播。
- Worker 是受信任官方程序，当前不是恶意插件的完整操作系统沙箱。

本地歌词/封面补全使用独立 `musicfs.scraper.local` 实例。示例已双层启用该插件，构建包含对应程序；配置、逐目标预览与人工选用见[补全任务](metadata.md)，无需启用FFmpeg或指纹。修改YAML后重启生效，不提供动态启停；`importLyrics`仍未开启自动补缺。
