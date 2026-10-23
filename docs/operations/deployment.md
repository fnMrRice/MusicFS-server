# 安装制品与部署

已有运行验证的环境为 Windows amd64 与 Arch WSL2 Linux amd64。其他发行版、实体 NAS、ARM64、Docker 和反向代理环境仍见[待验收清单](../validation/README.md)。容器配置仅为示例。

## 构建安装目录

使用各模块 go.mod 固定的 Go 工具链，在 Server 仓库根目录执行。Server、`sdk`、`plugins/source/online` 各有独立 go.mod；构建脚本强制 `GOWORK=off` 并使用 `-mod=readonly`，不依赖调用者的 Go workspace。目标目录必须不存在。正式构建要求工作树干净；`--allow-dirty` / `-AllowDirty` 只用于明确标记的验证制品。

```powershell
./scripts/release.ps1 -TargetOS windows -Output ./build/releases/windows-amd64 -WebRoot ../web/dist
./scripts/release.ps1 -TargetOS linux -Output ./build/releases/linux-amd64 -WebRoot ../web/dist
```

```sh
sh scripts/release.sh --os linux --output ./build/releases/linux-amd64 --web-root ../web/dist
```

Web 先在独立仓库按自己的工具链构建。`--web-root` 可省略；只复制静态制品，拒绝隐藏文件、链接、源码映射和超限文件，不复制 Web 源码或 Git。默认 `bin` 保留 `musicfs` 和 `musicfs-plugins` 两个文件（Windows 带 `.exe`），运行时只启动 `musicfs`，官方内置插件全部在主进程。后一个程序仅是显式外部进程模式的兼容入口，保留九种 `--plugin <用途>` 选择值。

需要外部兼容程序时，在 PowerShell 构建或发布命令加 `-SplitPlugins`，Shell 命令加 `--split-plugins`。此时额外输出 `musicfs-plugin-netease`、`musicfs-plugin-qqmusic`、`musicfs-plugin-applemusic`、`musicfs-plugin-online`、`musicfs-plugin-storage-sqlite`、`musicfs-plugin-dsp-gain`，合计八个程序；默认配置仍使用内置实现，构建文件本身不会启动插件进程。

发布构建移除调试信息。`release.json` 记录源提交、脏树状态、平台、版本/协议、三个源码模块和实际编译依赖，并在 `components[].args` 中记录角色；`SHA256SUMS` 覆盖所有交付文件。存在 `.incomplete` 表示失败，不可当成完成的制品。校验清单证明文件一致性，不代表有发布者签名。

安装包不包含 `docs/` 目录，运行、配置和协议文档保留在对应版本的源码仓库中。

三个在线平台由 Server 编译内的独立平台模块装配，每个用户与实例保持独立账号。普通插件默认 `execution: builtin`，不依赖旧路径是否存在；显式 `execution: process` 才使用统一或独立入口及摘要校验。发现范围和旧配置的 UUID、账号、禁用状态保留规则见[发现与迁移](configuration.md#内置插件程序与自动发现)。

SQLite 是必需基础设施，`release.json` 标记 `required: true`；生成配置默认 `storage.backend: sqlite`，在 Server 主进程打开主库与实例状态库，不需要数据库插件程序。数据库初始化失败仍拒绝启动，不提供禁用或旁路开关。只有显式外部配置才校验程序和摘要，旧官方 Worker 配置的迁移及外部身份见[存储插件](../plugins/storage-plugins.md)。

存储与可选 DSP 通过各自配置启动，不加入普通插件自动发现。默认 `audioDSP.chain/plugins` 均为空；官方 `musicfs.dsp.gain` 使用内置实现，登记名称及超时后配置处理链并重启，无需可执行路径或摘要，详见[DSP 插件](../plugins/dsp-plugins.md)。第三方外部节点仍须明确登记程序与固定摘要。音频工具 FFmpeg、ffprobe、fpcalc 可在主进程插件的控制下外部执行。

本版官方 FFmpeg 实现为 `0.2.0`，Server 仍精确校验握手；内置代码随 Server 升级，显式外部模式须同步升级对应程序和固定摘要。四类新增算法及其他插件各自维护发行版本，不依赖 Server 版本号代替。

配置示例自动改为 `../bin`，Windows 保留 `.exe`；内置插件默认自动发现，本地音乐默认没有选定的导入目录。允许访问的范围采用当前运行用户的 Music 目录及 Windows 首次探测结果，也可通过 `permissions.readDirectories` 显式覆盖；缺配置的插件显示待配置。复制为 `configs/server.yaml`，需要禁用时设置插件或实例 enabled: false，然后创建管理员令牌并启动。实际导入目录启动后从 Web 管理页面选择，保存不会自动扫描：

```powershell
$env:MUSICFS_ADMIN_TOKEN = (& ./bin/musicfs.exe token).Trim()
./bin/musicfs.exe serve --config ./configs/server.yaml
```

```sh
export MUSICFS_ADMIN_TOKEN="$(./bin/musicfs token)"
./bin/musicfs serve --config ./configs/server.yaml
```

令牌只保存在启动环境中，不写入 YAML、镜像或命令 URL。使用前保存到自己的凭据管理中，用于 Web 登录。首次扫描、原流与处理流验证见[运行说明](running.md)；按 Ctrl+C 或发送 SIGTERM 正常退出，日志输出英文且不记录凭据。生命周期归属默认为独立启动，桌面管理模式须通过单独的鉴权租约协议。

## 外部工具与许可

FFmpeg/ffprobe 不进入安装包。Windows 可使用 [FFmpeg 官方下载页](https://ffmpeg.org/download.html)列出的 Windows 构建，Linux 可用发行版包或审核后的源码构建。当前实测 Windows 6.1.1、Linux 6.1.6；需要 `libmp3lame` 编码器，并配置两个可执行路径后启用音频插件。启动探测读取版本、SHA-256、编码器与支持格式，能力不满足时明确失败；系统存在名为 ffmpeg 的命令不等于已通过能力验收。

Chromaprint `fpcalc` 1.6.1 是默认禁用的指纹功能所需外部工具，须填写审核后的 SHA-256；见[指纹说明](../metadata/identification.md#本地音频指纹)。安装其他版本并不会自动获得已验证状态。

`release.json.modules` 记录三个源码模块，包内保留各自 go.mod/go.sum。`dependencies` 记录依赖的模块归属，区分实际编译与仅在模块图/测试中使用的项目；缺少已编译第三方依赖许可证时打包失败。仅允许仓库内 SDK/在线模块的指定本地替换，并标记 firstParty 与 source。licenses 保留原许可；外部音频工具和容器系统另行核对。平台及 NCM 的参考项目不是运行服务。仓库与制品保持私有。

## 容器示例（尚未运行验收）

先构建 Linux 安装目录，再以该目录作为构建上下文：

```sh
docker build -f deploy/Dockerfile -t musicfs-server:local build/releases/linux-amd64
```

Dockerfile 默认基础镜像为 `debian:bookworm-slim`；正式使用前将 `BASE_IMAGE` 固定到审核的镜像摘要，记录 FFmpeg 的实际包版本/配置并运行同样的音频验收。本仓库未拉取或验证该镜像。镜像只复制程序、发布清单和许可证，不复制开发库、音乐、测试目录或凭据；不包含 Web，需要时把单独 Web 制品以只读卷挂载，并设置 `webRoot`。受限用户为 UID/GID 10001，Docker 的 USER/HEALTHCHECK/停止信号语义参考[官方文档](https://docs.docker.com/reference/dockerfile/)。

Linux 发布流程会将包内 `deploy/server.container.example.yaml` 改为 `storage.backend: plugin`，程序为 `/opt/musicfs/bin/musicfs-plugins`，参数为 `['--plugin', 'storage-sqlite']`，摘要取该 Linux 包的实际程序文件；缺少文件时拒绝打包。源码与 Windows 包内的容器示例保留内置 SQLite 兼容，不能替代 Linux 发布配置。这里的生成与摘要测试不代表已经运行 Docker。

把 `deploy/compose.example.yaml` 复制到独立安装目录。创建 `configuration/server.yaml`（取自容器 YAML 示例）、`music` 和 `tls` 目录；配置目录须允许 10001 写入，私钥只授予服务用户读取。`musicfs.env` 由管理员在该安装目录创建，只包含自己的 `MUSICFS_ADMIN_TOKEN`，不要加入 Git。TLS 证书须匹配浏览器使用的名字；示例健康检查使用 `localhost`，其证书也须包含该名称，CA 放 `tls/ca.pem`。端口仅发布到宿主 loopback。

data/media/cache 分别为持久卷，配置为持久挂载，音乐默认 `ro`。SQLite data 卷须使用本地可靠文件系统；音乐可绑定已挂载 NAS 的宿主路径，但容器配置只写容器内 `/music`。不要把数据库放在 NAS 上。Linux 原件写回当前不支持，不能通过改为 `rw` 就宣称可用。

运行 `docker compose up -d` 后，先确认健康检查和 `/api/v1/health/ready`，再登录、扫描与播放；`docker compose logs` 查看日志，`docker compose stop` 提供 30 秒退出宽限。存活路径为 `/api/v1/health/live`，就绪路径在 draining 时返回 503。示例 cache 卷不是磁盘配额；应用 1 GiB 快照上限不涵盖全部临时/系统文件，宿主需按实际容量管理。

## TLS、同源与代理

Server 对非 loopback 监听要求直接配置 TLS，不根据 `X-Forwarded-*` 推断安全性或授权。反向代理与 Server 之间也使用验证证书的 HTTPS；`deploy/nginx.example.conf` 是待替换域名/证书路径的样例。将页面实际 Origin 明确加入 Server 的 `allowedOrigins`，保持页面和 API 同源；不要添加通配来源。

代理关闭响应和请求缓冲，读/写空闲超时为 120 秒，上传外层限额为 16 MiB，Server 各接口仍执行自身限制。`/api/v1/control` 必须转发 HTTP/1.1 的 Upgrade/Connection 头，支持 `musicfs.control.v1` WebSocket；HTTPS 页面使用 WSS，CSP 的 connect-src 需允许对应受信任来源，不能用通配来源代替。连接鉴权、10 秒心跳及 30 秒超时见[控制协议](../contracts/control-channel.md)，本机生命周期仍检查实际 loopback 连接。

音频保持独立 HTTP 流，不经 WS；反向代理不能缓冲完整处理音频后才发送。配置依据 [Nginx 代理文档](https://nginx.org/en/docs/http/ngx_http_proxy_module.html)，实际 Nginx/TLS 与浏览器联调仍待验收。远程启用前验证证书、Cookie、Origin/首帧 CSRF、WS 升级/重连/登录撤销、长音频流、seek 和停止，健康检查不能代替播放验收。

升级、完整备份、新目录恢复和回滚见[备份恢复](backup.md)。对应制品和音乐原件应独立保存，不能仅复制数据库后丢弃人工媒体与恢复记录。
