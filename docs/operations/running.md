# 本地运行与扫描

在 Server 仓库根目录执行以下命令。正式安装包、容器及 TLS 配置见[部署](deployment.md)，字段定义见[配置](configuration.md)。

## 构建与启动

使用各模块 go.mod 固定的工具链。先在 Web 仓库执行 npm ci、npm run build，再构建服务端：

```powershell
./scripts/build.ps1
Copy-Item configs/server.example.yaml configs/server.local.yaml
```

Linux 使用 `sh scripts/build.sh`，将示例中的 ../build/windows/ 改为 ../build/linux/ 并去掉 .exe。默认生成 musicfs、musicfs-plugins 两个文件，但只启动 musicfs：全部官方内置插件在 Server 主进程运行。后者保留显式外部模式的九种兼容入口；分拆构建用 `-SplitPlugins` / `--split-plugins`。

编辑配置：

- Local Files 默认没有选定的音乐目录。允许访问的范围由默认授权或 permissions.readDirectories 决定，实际目录在启动后通过管理接口添加，见[目录授权](../plugins/plugin-configuration.md#部署授权)。
- FFmpeg 插件及实例两层启用，并配置 ffmpegPath、ffprobePath；缺少工具时保持待配置。当前原流支持兼容 WAV 及已识别容器、编码的 MP3，FLAC、AIFF 与 NCM 内部 FLAC 走处理路径。此前未配置 ffprobe 扫描的 MP3 可能缺少格式信息，补齐工具后重新扫描。NCM 还需启用解密插件及其实例。
- 普通插件默认 `execution: builtin`，无需程序路径。只有显式 `execution: process` 才使用 executable、args 和程序摘要。缺配置保持待配置，显式列表及禁用优先，见[自动发现](configuration.md#内置插件程序与自动发现)。
- 数据、媒体、缓存与音乐目录分离。相对路径以 YAML 所在目录为基准；修改部署权限后重启。
- allowedOrigins 与页面实际 URL 完全一致。远程监听必须配置 TLS；静态页面与 API 保持同源。

Windows：

```powershell
$env:MUSICFS_ADMIN_TOKEN = (& ./build/windows/musicfs.exe token).Trim()
./build/windows/musicfs.exe serve --config ./configs/server.local.yaml --web-root (Resolve-Path ../web/dist).Path
```

Linux：

```sh
export MUSICFS_ADMIN_TOKEN="$(./build/linux/musicfs token)"
./build/linux/musicfs serve --config ./configs/server.local.yaml --web-root "$(realpath ../web/dist)"
```

将生成的管理员凭据用于 Web 登录，保存在自己的凭据管理中，不写入配置、URL 或 Git。服务保持前台运行，Ctrl+C 正常退出；默认独立启动，客户端托管模式见[生命周期](lifecycle.md)。

默认 `info` 日志同时写控制台与 `dataRoot/logs/server.log`，插件各自写入 `dataRoot/logs/plugins/<pluginId>.log`。同一 UTC 日期的服务端与插件历史切片合并压缩为 `dataRoot/logs/logs-YYYY-MM-DD.tar.gz`，保留30天；临时排错可通过管理员接口开启 `debug/trace`，目录、等级配置与接口说明见[日志](logging.md)。

## 在局域网发现 Server

下面的部署片段让客户端在局域网找到监听 8443 端口的 Server。将示例域名替换为局域网 DNS 可解析的地址，配置与该名称匹配且客户端信任的证书。也可使用证书覆盖的局域网 IP 字面量，IPv6 Origin 写成 `https://[fd00::20]:8443`。

```yaml
listen: '0.0.0.0:8443'
tlsCert: '/etc/musicfs/server.crt'
tlsKey: '/etc/musicfs/server.key'
allowedOrigins: ['https://music.home.example:8443']
discovery:
  enabled: true
  origin: 'https://music.home.example:8443'
```

客户端在本地链路浏览 `_musicfs._tcp.local.`。SRV 记录包含当前监听器端口，TXT 提供 `api=1`、`scheme=https` 和选定的 `origin`。每块参与发布的网卡只通告自身监听地址。DNS-SD 记录公开可见，只帮助选定地址，登录仍需通过 HTTPS 校验证书、Server 身份和凭据。仅监听回环时跳过发布。没有匹配端口的允许 HTTPS Origin 时也跳过发布，并记录配置诊断；HTTP 服务正常运行。退出时撤销已发布的服务。

允许同一链路的 mDNS 多播 UDP 5353 及所配置的 HTTPS TCP 端口。Wi-Fi 客户端隔离、组播过滤或跨网段时，可以手工输入相同的 HTTPS Origin。反向代理外的用户使用管理员提供的公开地址；本机广播使用 Server 实际监听端口。

## 选择音乐目录与扫描

登录 http://127.0.0.1:8080，在“设置 → 音乐源”的“本地音乐”中添加授权根或其子目录。逐目录保存名称、递归深度、隐藏内容及 .nomedia 设置；默认递归、不限深度，跳过隐藏内容并遵循 .nomedia。Web 在添加或编辑配置应用成功后自动提交扫描。删除目录只移除配置，保留授权范围、磁盘文件和曲库记录，不触发扫描。

插件管理中的目录数量统计允许访问的根，与已添加目录数不同。目录设置保存在插件配置数据库；YAML 权限限定授权范围，旧 config.musicRoots 继续兼容。

需要手动重扫时，点击「音乐源 → 本地音乐」标题行的「扫描音乐」。自动和手动扫描均覆盖当前实例所有已添加目录，沿用各目录查找设置；页面展示排队、已发现曲目数和最终结果，完整成功后刷新曲库。没有已添加目录或来源尚未就绪时不可提交。连接中断可继续确认原任务，服务端重试等待不需要重复提交；暂停或失败时按提示处理后重新扫描。直接调用配置 API 只保存配置，需要调用扫描 API 或脚本完成导入。

也可在另一个设置了相同 MUSICFS_ADMIN_TOKEN 的终端中运行：

```powershell
./scripts/scan.ps1 -ProviderID b1548e96-3081-5c9c-8376-bf290e369d51
./scripts/scan.ps1 -ProviderID b1548e96-3081-5c9c-8376-bf290e369d51 -RootIDs local -Recursive:$true -MaxDepth 2
```

ProviderID 使用实际实例 UUID；RootIDs 来自 Provider 的 roots，不是磁盘路径。默认扫描该实例可导入的全部根；MaxDepth 0 表示不限深度，Recursive false 表示只查根本层。脚本以临时登录会话读取导入声明和根，提交完整扫描请求并等待，结束后退出该会话。Linux 可使用 PowerShell 7，或按 OpenAPI 调用相同接口。

排除规则、单次歌词导入、监听与周期复核见[扫描](../library/scanning.md)。retry_wait 等待原任务调度；paused 先处理依赖或权限原因，再显式重提。脚本超时不等于任务取消，应按返回的任务 ID 查询。

## 播放与数据

扫描后从 Web 曲目列表选择音频，按返回的实际能力播放与定位。NCM 原件保持只读：内部 MP3 使用固定解密表示，当前内部 FLAC 经 FFmpeg 处理；格式限制见[NCM](../playback/ncm.md)。处理后定位使用新尝试及曲目起点映射，资源限制和缓存见[音频处理](../playback/audio-processing.md)、[处理缓存](../playback/processed-cache.md)。

SQLite WAL 放本地文件系统；音乐可来自已挂载目录。根身份变化或无法打开时保留旧库，不通过删除状态文件规避检查。备份前正常停服，使用 backup、verify-backup、restore 命令，精确流程见[备份恢复](backup.md)。

歌词/封面由独立本地 Scraper 补全；自动补缺与原件写回默认关闭。普通插件启用、实例设置可动态应用，停用只保存意图，重启后生效。平台账号、在线整曲及未测环境见[待验收清单](../validation/README.md)。
