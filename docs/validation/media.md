# S-P6-01 媒体文件统一验收

日期：2026-10-10。验证源码树为 `9f22083e83152cb4b15c4c33315ef7bb41f2d39d`，在忽略的独立副本运行，未混入后续 Scraper/GC 开发内容。API、配置启动与内容权限的细节见[媒体 HTTP 验收](media-http.md)。

| 环境 | 执行与结果 |
| --- | --- |
| Windows amd64 / Go 1.26.9 | `scripts/check.ps1 -Race` 通过，包含格式、vet、全量覆盖率测试和全部 tests 的 race；普通 E2E 118.822 秒、race E2E 130.921 秒，race storage 125.046 秒 |
| Arch Linux / WSL2 amd64 | 全量 vet、`go test ./tests/...` 通过；E2E 256.655 秒、真实插件 193.510 秒 |
| Linux 关键包 race | unit/mediastore 1.209 秒、integration/mediastore 4.413 秒、storage 58.638 秒、recovery 1.036 秒、contract/api 36.881 秒、e2e 223.829 秒，均通过 |

两平台均配置实际 FFmpeg、ffprobe 和 fpcalc，真实音频用例未因工具未配置而跳过。Windows 使用已安装 FFmpeg 6.1.1；Linux 使用项目忽略目录内从官方源码构建的 FFmpeg 6.1.6（含合成夹具所需的 FLAC 编码器），Chromaprint 1.6.1 与工具校验值沿用[指纹验收](fingerprinting.md)。Linux 测试临时数据位于 WSL 原生 `/tmp`，源码位于 Windows 磁盘挂载；这些结果不代表物理 NAS、其他发行版或 ARM 已验收，也不需要更改系统 FFmpeg 安装。

内容服务另外验证丢失的新预览不能替换有效选择，已成功的幂等请求不因之后文件丢失而重复执行。对象丢失会保留人工锁并推进引用修订；历史对象保持归属隔离。正文位于 mediaRoot，数据库仅记录对象描述、摘要、来源、目标和引用。

本步实现来源资产范围的 UTF-8 纯文本/LRC 与 JPEG/PNG。发行/来源专辑绑定、图片缩略图、完整刮削文档、自动抓取、写回及完整部署不由本记录宣称完成。Windows 和 Linux 的自动化文件/服务测试不能替代人工听音或浏览器编辑器验收。
