# S-P8 制品与安装验证

日期：2026-10-10。使用独立临时安装目录、项目合成音频和临时管理员会话。没有读取用户真实歌曲、执行平台账号登录或发布制品。

## 打包与许可边界

`scripts/release` 通过固定Go模块分别构建八个正式程序，显式复制配置/用户说明/协议/扫描脚本，可选复制独立Web静态制品。文档只取受版本控制的白名单，不包括ignored/untracked文档、开发实施记录或私人验收日志。Go实际工具链的LICENSE/PATENTS和已编译模块的原许可随包保存；模块路径/版本/摘要与compiled标志写入清单。参考项目只在文档说明，不冒充二进制运行依赖。

Windows审阅验证包的78个文件已逐个对照SHA256SUMS通过；未携带validation/implementation目录，Go许可与扫描脚本存在。初版复制所有docs的问题已在交付前收紧。配置中的普通示例路径不是个人机器目录；检查未发现个人用户名、实际测试音乐目录或凭据进入收紧后的文档。

前述首次记录来自开发期间标明dirty的验证构建。最终已从干净提交 `73f2d10` 生成 Windows/Linux amd64 制品：八个程序、schemaVersion=21、每包97条文件校验，`dirty=false`，无`.incomplete`标记。分别运行包内程序的安装检查通过10.815秒和15.968秒，音频标记和退出行为与下文一致，见[最终联合验证](final-validation.md)。没有签名、上传或公开分发；没有将private安装验证说成已经公开发布。

## 真实安装流程

`tests/e2e/release_test.go` 由 `MUSICFS_RELEASE_ROOT` 显式指定制品。先验证校验清单，再复制到全新临时目录，执行制品里的Server与独立Worker；测试不另建替代程序。配置来源为制品示例，复核默认所有插件关闭后才启用本地、NCM、Scraper和已配置工具的音频实例。

- Windows正式安装程序流程通过，4.049秒（主体3.25秒）。真实扫描3条合成音频，WAV/NCM Range逐字节相符，FLAC处理后6秒音频和4秒定位的2秒片段均解码出660 Hz标记，RMS 0.11874。
- WSL2 Arch Linux相同流程通过，主体4.45秒，随生命周期与备份联合E2E race包共70.657秒。相同PCM标记和RMS通过。
- 两边均实际提供打包的静态Web首页、保持输入字节不变；`client-managed`最后租约释放后真实CLI进程正常退出。

两套制品由Windows Go 1.26.9编译为各自amd64目标；Linux程序随后在WSL原生执行，构建与运行证据分开。音频使用Windows FFmpeg/ffprobe 6.1.1与WSL 6.1.6，均为显式外部依赖。此项是HTTP/实际解码验证，不等于浏览器有声验收或人工扬声器听音。

## 未验证范围

直接TLS另用`TestActualTLSCookieCSRFRangeAndRevocation`验证，Windows race10.829秒、Linux race27.411秒通过。启动真实配置的HTTPS Server，用临时CA信任池验证证书（不跳过验证、不写系统信任库）；登录Cookie具备Secure/HttpOnly/SameSite保护，伪造转发头不能绕过Origin/CSRF，真实扫描/Range与撤销后401通过，输入保持不变。

Dockerfile、Compose、Nginx配置已提供并按官方指令语义复核，尚无本机Docker引擎或容器联调证据。ARM64、macOS、实体NAS、Linux浏览器、其他发行版与跨平台数据搬迁不在当前已验证矩阵内。具体配置和完整安装步骤见[部署说明](../deployment.md)，数据备份见[恢复说明](../backup.md)。
