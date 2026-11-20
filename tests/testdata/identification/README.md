# 指纹匹配测试向量

[candidate-fingerprints.json](candidate-fingerprints.json) 包含 14 组数学合成音频的 Chromaprint 原始输出，按 CC0-1.0 提供。Go 单元测试直接读取这些向量。字段 `name` 标识变体，`pcmSha256` 校验规范 PCM 字节；完整源文件的匹配另用 SHA-512。

## 样本与生成参数

基准样本是一段 32 秒的合成旋律、和弦与节奏。变体覆盖 FLAC、MP3（64/192 kbit/s）、AAC（128 kbit/s）、3 秒前导静音、10–30 秒片段、不同演绎、变速和回声、混音、不同旋律、静音以及 440/880 Hz 纯音。

FFmpeg 6.1.1 将输入转为单声道、11025 Hz、s16le PCM。使用 `fpcalc 1.6.1` 从标准输入读取：

```sh
fpcalc -format s16le -rate 11025 -channels 1 -algorithm 2 -length 0 -raw -json -
```

Linux 上使用 FFmpeg 6.1.6 复核过 PCM 字节与指纹。[工具来源与许可](../../../src/metadata/identification/fingerprint/REFERENCES.md)

## 已知反例

440 Hz 与 880 Hz 的纯音产生相同指纹，另一种合成演绎也能进入待确认列表。这些样本用于验证比较边界，真实录音的识别准确率仍需独立语料评估。[候选评分规则](../../../src/metadata/identification/candidates/README.md)
