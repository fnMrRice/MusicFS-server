# NAS / Bilibili 开源参考记录

核查日期：2026-10-10。仅核查公开仓库说明与可读取源码；未引入依赖、复制代码或运行 NAS / B 站联调。下面的分支链接不是锁定版本，实施前必须记录 commit SHA、许可证正文与采用文件；未读到的许可内容不得按文件名推断已完成审查。

| 用途 | 优先参考 | 本次证据及采用计划 |
| --- | --- | --- |
| SMB | [hirochachacha/go-smb2](https://github.com/hirochachacha/go-smb2)，仓库标注 BSD-2-Clause | [v1.1.0 README](https://github.com/hirochachacha/go-smb2/blob/v1.1.0/README.md)有共享、文件与上下文示例；优先评估复用 Go 库。当前 master 的 v2 文档要求 Go 1.27，而本项目 go.mod 为 Go 1.26，不能直接选择浮动最新版；先核对兼容版本与所需安全能力，升级工具链需单独记录 |
| NFS | [willscott/go-nfs-client](https://github.com/willscott/go-nfs-client) | [nfs/nfs.go](https://github.com/willscott/go-nfs-client/blob/master/nfs/nfs.go)为协议适配调查入口；候选用于 NFSv3，只读功能、取消与稳定性待 PoC。仓库可见 `LICENSE_BSD-2.txt` / `NOTICE.txt`，本次正文获取失败，许可确认作为引入前门槛；不声称支持 NFSv4 |
| B 站音频 | [yt-dlp/yt-dlp](https://github.com/yt-dlp/yt-dlp)，源码 [Unlicense](https://github.com/yt-dlp/yt-dlp/blob/master/LICENSE) | [bilibili.py](https://github.com/yt-dlp/yt-dlp/blob/master/yt_dlp/extractor/bilibili.py)可作为视频音轨、`au` 曲目、`am` 音频歌单的流程参考；优先 Go 适配，不默认引入 Python 或二进制工具。若分发其制品，另核对所选构建的第三方许可 |

参考源码存在能力不等于当前平台线上可用。音频歌单参考代码出现 `pn=1, ps=100` 的读取，不能直接当作本项目完整歌单同步实现；必须补齐翻页及完整性验收。收藏夹是独立实施目标，需要另固定可核查的收藏夹分页参考与脱敏响应样例，不能用音频歌单接口代替。

[Nemo2011/bilibili-api](https://github.com/Nemo2011/bilibili-api)页面显示已于 2026-07-06 归档，本次不将其列为活跃主依赖；若采用历史接口或后继项目，重新核查仓库、许可与版本。

每次采用记录至少包括：上游 URL、tag/commit、文件与模块、许可证/NOTICE、直接依赖/移植/仅参考的方式、改动范围、更新策略、脱敏测试样例及真实环境结果。优先复用已验证库和接口流程，不重写 SMB/NFS 协议栈；平台授权与内容权限仍按实际账号和资源判断。
