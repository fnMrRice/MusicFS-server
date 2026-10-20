# 安装制品与部署

当前支持范围为 Windows amd64 与 Arch WSL2 Linux amd64，具体运行证据见[验证记录](validation/progress.md)。其他 Linux 发行版、实体 NAS、ARM64 和 Docker 运行时仍需各自验收。下面提供容器配置，但本机没有 Docker 引擎，不能把配置文件检查描述为容器运行成功。

## 构建安装目录

使用各模块 go.mod 固定的 Go 工具链，在 Server 仓库根目录执行。Server、`sdk`、`plugins/source/online` 各有独立 go.mod；构建脚本强制 `GOWORK=off` 并使用 `-mod=readonly`，不依赖调用者的 Go workspace。目标目录必须不存在。正式构建要求工作树干净；`--allow-dirty` / `-AllowDirty` 只用于明确标记的验证制品。

```powershell
./scripts/release.ps1 -TargetOS windows -Output ./build/releases/windows-amd64 -WebRoot ../web/dist
./scripts/release.ps1 -TargetOS linux -Output ./build/releases/linux-amd64 -WebRoot ../web/dist
```

```sh
sh scripts/release.sh --os linux --output ./build/releases/linux-amd64 --web-root ../web/dist
```

Web 先在独立仓库按自己的工具链构建。`--web-root` 可省略；只复制静态制品，拒绝隐藏文件、链接、源码映射和超限文件，不复制 Web 源码或 Git。制品中的 `bin` 包含八个程序（Windows 带 `.exe`）：`musicfs`、`musicfs-plugins`、`musicfs-plugin-netease`、`musicfs-plugin-qqmusic`、`musicfs-plugin-applemusic`、`musicfs-plugin-online`、`musicfs-plugin-storage-sqlite`、`musicfs-plugin-dsp-gain`。发布构建移除调试信息；统一插件程序的四种本地角色通过 `components[].args` 选择，在线平台、存储和 DSP 程序使用各自入口。`release.json` 记录源提交、脏树状态、平台、版本/协议、三个源码模块和实际编译依赖，`SHA256SUMS` 覆盖所有交付文件。存在 `.incomplete` 表示失败，不可当成完成的制品。校验清单证明文件一致性，不代表有发布者签名。

安装包不包含 `docs/` 目录，运行、配置和协议文档保留在对应版本的源码仓库中。

三个在线平台同时提供独立程序和 `musicfs-plugin-online --plugin netease|qqmusic|applemusic` 统一入口。自动发现优先使用对应独立程序，缺失时由同目录统一程序补足，按相同插件 ID 去重；统一程序的各平台仍分别启动进程。显式禁用和实例身份不因入口选择改变；已固定的程序摘要不会自动清除，详见[发现与迁移](configuration.md#内置插件程序与自动发现)。

新安装包将 SQLite 独立存储程序作为必需基础设施，`release.json` 标记为 `required: true`；生成的配置固定 `storage.backend: plugin`、`../bin/musicfs-plugin-storage-sqlite(.exe)` 及本次打包文件的实际 SHA-256，缺少文件时打包失败。它不进入普通插件启停列表、不提供禁用开关；程序缺失、校验失败或加载失败时拒绝启动。源仓库示例和未声明 storage 的旧配置保留内置 SQLite 兼容，迁移说明见[存储插件](storage-plugins.md)。

存储与可选 DSP 程序均不加入 `musicfs-plugins` 的角色自动发现，直接使用各自入口。默认 `audioDSP.chain/plugins` 均为空；需要 DSP 时，在部署 YAML 的 `audioDSP` 中指定 `../bin/musicfs-plugin-dsp-gain(.exe)` 及实际 SHA-256，再重启，详见[DSP 插件](dsp-plugins.md)。更新二进制后须同步固定摘要。

本版官方 FFmpeg Worker 为 `0.2.0`，Server 要求精确匹配，因此 `musicfs` 与 `musicfs-plugins` 须同时升级；不能把旧版统一插件程序与新版 Server 混用，即使尚未启用额外 DSP。其他插件角色的实现版本保持各自记录值。

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

Chromaprint `fpcalc` 1.6.1 是默认禁用的指纹功能所需外部工具，须填写审核后的 SHA-256；见[指纹说明](fingerprinting.md)。安装其他版本并不会自动获得已验证状态。

`release.json.modules` 列出 Server、SDK 和在线插件三个源码模块，包内保留各自的 go.mod/go.sum。`dependencies` 合并三个模块的依赖图并记录每项的 `modules` 归属，区分 `compiled:true` 的二进制模块与只在模块图/测试中存在的项目；实际编译的第三方依赖必须收集到许可文件，否则打包失败。源码中的 SDK/在线插件本地替换仅允许指向仓库中对应的真实目录，清单标记 `firstParty` 与 `source`，适用根 LICENSE，不伪装为遗漏许可的第三方依赖。其他本地或远程替换会拒绝打包。`licenses` 保留依赖原许可，根 LICENSE 保留本仓库许可。README 的 NCM/在线平台项目属于格式或设计参考，见[平台能力](platform-capabilities.md)；它们不被列为实际运行服务。Go 标准库随工具链编译；FFmpeg、fpcalc、容器基础系统及其包使用各自来源的许可，制品不替代对应来源材料。仓库与生成制品保持私有，不自动上传或发布。

## 容器示例（尚未运行验收）

先构建 Linux 安装目录，再以该目录作为构建上下文：

```sh
docker build -f deploy/Dockerfile -t musicfs-server:local build/releases/linux-amd64
```

Dockerfile 默认基础镜像为 `debian:bookworm-slim`；正式使用前将 `BASE_IMAGE` 固定到审核的镜像摘要，记录 FFmpeg 的实际包版本/配置并运行同样的音频验收。本仓库未拉取或验证该镜像。镜像只复制程序、发布清单和许可证，不复制开发库、音乐、测试目录或凭据；不包含 Web，需要时把单独 Web 制品以只读卷挂载，并设置 `webRoot`。受限用户为 UID/GID 10001，Docker 的 USER/HEALTHCHECK/停止信号语义参考[官方文档](https://docs.docker.com/reference/dockerfile/)。

Linux 发布流程会将包内 `deploy/server.container.example.yaml` 改为 `storage.backend: plugin`，程序为 `/opt/musicfs/bin/musicfs-plugin-storage-sqlite`，摘要取该 Linux 包的实际 Worker 文件；缺少文件时拒绝打包。源码与 Windows 包内的容器示例保留内置 SQLite 兼容，不能替代 Linux 发布配置。这里的生成与摘要测试不代表已经运行 Docker。

把 `deploy/compose.example.yaml` 复制到独立安装目录。创建 `configuration/server.yaml`（取自容器 YAML 示例）、`music` 和 `tls` 目录；配置目录须允许 10001 写入，私钥只授予服务用户读取。`musicfs.env` 由管理员在该安装目录创建，只包含自己的 `MUSICFS_ADMIN_TOKEN`，不要加入 Git。TLS 证书须匹配浏览器使用的名字；示例健康检查使用 `localhost`，其证书也须包含该名称，CA 放 `tls/ca.pem`。端口仅发布到宿主 loopback。

data/media/cache 分别为持久卷，配置为持久挂载，音乐默认 `ro`。SQLite data 卷须使用本地可靠文件系统；音乐可绑定已挂载 NAS 的宿主路径，但容器配置只写容器内 `/music`。不要把数据库放在 NAS 上。Linux 原件写回当前不支持，不能通过改为 `rw` 就宣称可用。

运行 `docker compose up -d` 后，先确认健康检查和 `/api/v1/health/ready`，再登录、扫描与播放；`docker compose logs` 查看日志，`docker compose stop` 提供 30 秒退出宽限。存活路径为 `/api/v1/health/live`，就绪路径在 draining 时返回 503。示例 cache 卷不是磁盘配额；应用 1 GiB 快照上限不涵盖全部临时/系统文件，宿主需按实际容量管理。

## TLS、同源与代理

Server 对非 loopback 监听要求直接配置 TLS，不根据 `X-Forwarded-*` 推断安全性或授权。反向代理与 Server 之间也使用验证证书的 HTTPS；`deploy/nginx.example.conf` 是待替换域名/证书路径的样例。将页面实际 Origin 明确加入 Server 的 `allowedOrigins`，保持页面和 API 同源；不要添加通配来源。

代理关闭响应缓冲和请求缓冲，读/写空闲超时设为 120 秒，上传外层限额为 16 MiB（Server 各接口仍执行更严格的限制）。这些设置按 [Nginx 官方代理文档](https://nginx.org/en/docs/http/ngx_http_proxy_module.html)编写，尚未完成实际 Nginx/TLS 容器联调。Server直接TLS已在Windows/WSL通过真实证书链校验、Secure/HttpOnly/SameSite Cookie、Origin/CSRF拒绝、Range和注销撤销测试；这不替代代理与浏览器部署验收。正式远程启用前验证长音频流、seek、Cookie及停止，不以健康 HTTP 200 代替有声验收。

升级、完整备份、新目录恢复和回滚见[备份恢复](backup.md)。对应制品和音乐原件应独立保存，不能仅复制数据库后丢弃人工媒体与恢复记录。
