# 本地指纹提取的算法来源

本页记录官方 Chromaprint 插件的算法来源。公共 `fingerprint` 模块接受提取器声明的算法名称、版本和 `analyzer`/`decoder` 工具身份；以下具体版本限制由 `plugins/analysis/chromaprint` 实现，宿主不据此拒绝其他提取器。

官方插件以 Go 标准库控制独立 Chromaprint `fpcalc` **1.6.1**，不内嵌其实现，不新增 Go/cgo、Node 或 Python 运行依赖。工具路径与 SHA-256 必须显式提供，启动和每次提取均校验；缺少工具时该能力不可用，不改变播放能力。

- 官方版本：[v1.6.1](https://github.com/acoustid/chromaprint/releases/tag/v1.6.1)，对应源码提交 `aed8eba2202dd9d7b3b0a56c77904cc805490d72`。
- [CLI 实现](https://github.com/acoustid/chromaprint/blob/v1.6.1/src/cmd/fpcalc.cpp)：使用 `-algorithm 2 -raw -json`，输入仅为 stdin 的 `s16le/mono/11025 Hz`。CLI 算法 2 对应 C API 枚举值 1，不能混用编号。
- [算法参数](https://github.com/acoustid/chromaprint/blob/v1.6.1/src/fingerprinter_configuration.h)：指纹步长约 123.81 ms，不表示精确播放时间轴。
- [许可说明](https://github.com/acoustid/chromaprint/blob/v1.6.1/LICENSE.md)：其自有源码为 MIT，包含的 FFmpeg 代码采用 LGPL-2.1；上游要求整体按 LGPL-2.1 看待。若分发工具二进制，应另附许可和对应源码获取材料，不能称为纯 MIT 工具。

输入由受控音频 Worker 在用户增益、EQ、变速之前规范化；请求窗口为 10–120 秒，起点不超过 24 小时。两端向下量化为采样位置，文件尾不补零。明细保存实际样本数量、规范化版本、工具版本和摘要；工具 stdout 的 duration 不用于覆盖范围。

可用质量门槛要求实际至少 10 秒、至少 8 种原始指纹值，且单值占比低于 90%。该保守过滤只排除短片、纯音及明显低信息片段，没有识别准确率或概率含义。所有指纹相似性均只能支持待确认候选，不能自动合并 Recording 或生成 TimelineEvidence。本模块不实现网络检索或上传。

执行上限：请求最多 120 秒规范 PCM、最多 1,000 项原始 uint32、工具 stdout 和明细各最多 64 KiB、stderr 最多 16 KiB、任务默认 30 秒且配置不超过 120 秒。音频 Worker 自身规范化上限为 30 秒；工具子进程退出和取消必须回收，调用者另外持有原始资产的稳定读保护直至持久发布结束。

后台提取默认并发 1、最多 2。音频 Processor 固定另设 1 个规范化通道，不占用前台播放通道；额外规范化请求只在自身截止时间内等待，不额外启动 Worker。该隔离保证播放准入配额可用，不代表操作系统 CPU 或磁盘带宽的硬隔离。
