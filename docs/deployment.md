# 安装制品与部署

当前支持范围为 Windows amd64 与 Arch WSL2 Linux amd64，具体运行证据见[验证记录](validation/progress.md)。其他 Linux 发行版、实体 NAS、ARM64 和 Docker 运行时仍需各自验收。下面提供容器配置，但本机没有 Docker 引擎，不能把配置文件检查描述为容器运行成功。

## 构建安装目录

使用根 go.mod 固定的 Go 工具链，在 Server 仓库根目录执行。目标目录必须不存在。正式构建要求工作树干净；`--allow-dirty` / `-AllowDirty` 只用于明确标记的验证制品。

```powershell
./scripts/release.ps1 -TargetOS windows -Output ./build/releases/windows-amd64 -WebRoot ../web/dist
./scripts/release.ps1 -TargetOS linux -Output ./build/releases/linux-amd64 -WebRoot ../web/dist
```

```sh
sh scripts/release.sh --os linux --output ./build/releases/linux-amd64 --web-root ../web/dist
```

Web 先在独立仓库按自己的工具链构建。`--web-root` 可省略；只复制静态制品，拒绝隐藏文件、链接、源码映射和超限文件，不复制 Web 源码或 Git。制品中的 `bin` 包含 `musicfs` 与 `musicfs-plugins` 两个程序（Windows 带 `.exe`）；发布构建移除调试信息，七种插件的启动参数记录在 `components[].args`；`release.json` 记录源提交、脏树状态、平台、版本/协议和实际编译依赖，`SHA256SUMS` 覆盖所有交付文件。存在 `.incomplete` 表示失败，不可当成完成的制品。校验清单证明文件一致性，不代表有发布者签名。

安装包不包含 `docs/` 目录，运行、配置和协议文档保留在对应版本的源码仓库中。

配置示例自动改为 `../bin`，Windows 保留 `.exe`；内置插件默认自动发现，音乐根为待替换的 `../music`；缺配置的插件显示待配置。复制为 `configs/server.yaml`，配置实际音乐路径，需要禁用时设置插件或实例 enabled: false，然后创建管理员令牌并启动：

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

`release.json.dependencies` 区分 `compiled:true` 的二进制模块与只在模块图/测试中存在的项目；编译依赖必须收集到许可文件，否则打包失败。`licenses` 保留依赖原许可，根 LICENSE 保留本仓库许可。README 的 NCM/在线平台项目属于格式或设计参考，见[平台能力](platform-capabilities.md)；它们不被列为实际运行服务。Go 标准库随工具链编译；FFmpeg、fpcalc、容器基础系统及其包使用各自来源的许可，制品不替代对应来源材料。仓库与生成制品保持私有，不自动上传或发布。

## 容器示例（尚未运行验收）

先构建 Linux 安装目录，再以该目录作为构建上下文：

```sh
docker build -f deploy/Dockerfile -t musicfs-server:local build/releases/linux-amd64
```

Dockerfile 默认基础镜像为 `debian:bookworm-slim`；正式使用前将 `BASE_IMAGE` 固定到审核的镜像摘要，记录 FFmpeg 的实际包版本/配置并运行同样的音频验收。本仓库未拉取或验证该镜像。镜像只复制程序、发布清单和许可证，不复制开发库、音乐、测试目录或凭据；不包含 Web，需要时把单独 Web 制品以只读卷挂载，并设置 `webRoot`。受限用户为 UID/GID 10001，Docker 的 USER/HEALTHCHECK/停止信号语义参考[官方文档](https://docs.docker.com/reference/dockerfile/)。

把 `deploy/compose.example.yaml` 复制到独立安装目录。创建 `configuration/server.yaml`（取自容器 YAML 示例）、`music` 和 `tls` 目录；配置目录须允许 10001 写入，私钥只授予服务用户读取。`musicfs.env` 由管理员在该安装目录创建，只包含自己的 `MUSICFS_ADMIN_TOKEN`，不要加入 Git。TLS 证书须匹配浏览器使用的名字；示例健康检查使用 `localhost`，其证书也须包含该名称，CA 放 `tls/ca.pem`。端口仅发布到宿主 loopback。

data/media/cache 分别为持久卷，配置为持久挂载，音乐默认 `ro`。SQLite data 卷须使用本地可靠文件系统；音乐可绑定已挂载 NAS 的宿主路径，但容器配置只写容器内 `/music`。不要把数据库放在 NAS 上。Linux 原件写回当前不支持，不能通过改为 `rw` 就宣称可用。

运行 `docker compose up -d` 后，先确认健康检查和 `/api/v1/health/ready`，再登录、扫描与播放；`docker compose logs` 查看日志，`docker compose stop` 提供 30 秒退出宽限。存活路径为 `/api/v1/health/live`，就绪路径在 draining 时返回 503。示例 cache 卷不是磁盘配额；应用 1 GiB 快照上限不涵盖全部临时/系统文件，宿主需按实际容量管理。

## TLS、同源与代理

Server 对非 loopback 监听要求直接配置 TLS，不根据 `X-Forwarded-*` 推断安全性或授权。反向代理与 Server 之间也使用验证证书的 HTTPS；`deploy/nginx.example.conf` 是待替换域名/证书路径的样例。将页面实际 Origin 明确加入 Server 的 `allowedOrigins`，保持页面和 API 同源；不要添加通配来源。

代理关闭响应缓冲和请求缓冲，读/写空闲超时设为 120 秒，上传外层限额为 16 MiB（Server 各接口仍执行更严格的限制）。这些设置按 [Nginx 官方代理文档](https://nginx.org/en/docs/http/ngx_http_proxy_module.html)编写，尚未完成实际 Nginx/TLS 容器联调。Server直接TLS已在Windows/WSL通过真实证书链校验、Secure/HttpOnly/SameSite Cookie、Origin/CSRF拒绝、Range和注销撤销测试；这不替代代理与浏览器部署验收。正式远程启用前验证长音频流、seek、Cookie及停止，不以健康 HTTP 200 代替有声验收。

升级、完整备份、新目录恢复和回滚见[备份恢复](backup.md)。对应制品和音乐原件应独立保存，不能仅复制数据库后丢弃人工媒体与恢复记录。
