# 本地运行与扫描

在 Server 仓库根目录执行以下命令。正式安装包、容器及 TLS 配置见[部署](deployment.md)，字段定义见[配置](configuration.md)。

## 构建与启动

使用各模块 go.mod 固定的工具链。先在 Web 仓库执行 npm ci、npm run build，再构建服务端：

```powershell
./scripts/build.ps1
Copy-Item configs/server.example.yaml configs/server.local.yaml
```

Linux 使用 `sh scripts/build.sh`，将示例中的 ../build/windows/ 改为 ../build/linux/ 并去掉 .exe。默认只生成 musicfs、musicfs-plugins 两个程序；九种插件角色分别启动进程。分拆构建用 `-SplitPlugins` / `--split-plugins`。

编辑配置：

- Local Files 默认没有选定的音乐目录。允许访问的范围由默认授权或 permissions.readDirectories 决定，实际目录在启动后通过管理接口添加，见[目录授权](../plugins/plugin-configuration.md#部署授权)。
- FFmpeg 实例配置 ffmpegPath、ffprobePath；缺少工具时保持待配置。仅播放兼容原流无需启用处理插件；AIFF 与当前 NCM FLAC 处理路径需要两种工具。
- 普通插件 executable 指向统一程序，args 选择角色。缺配置保持待配置，显式禁用优先，见[自动发现](configuration.md#内置插件程序与自动发现)。
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

## 选择音乐目录与扫描

登录 http://127.0.0.1:8080，在“设置 → 音乐源”的“本地音乐”中添加授权根或其子目录。逐目录保存名称、递归深度、隐藏内容及 .nomedia 设置；默认递归、不限深度，跳过隐藏内容并遵循 .nomedia。删除目录只移除配置，保留授权范围、磁盘文件和曲库记录。保存不自动扫描。

插件管理中的目录数量统计允许访问的根，与已添加目录数不同。目录设置保存在插件配置数据库；YAML 权限限定授权范围，旧 config.musicRoots 继续兼容。

在另一个设置了相同 MUSICFS_ADMIN_TOKEN 的终端中运行：

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
