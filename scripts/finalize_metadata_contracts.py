"""Align analyzer terminology and migration documentation."""
from pathlib import Path

root = Path(__file__).resolve().parents[1]
paths = list((root / "plugins/analysis/chromaprint").glob("*.go"))
paths += [root / "src/app/bootstrap/fingerprinting.go", root / "tests/integration/plugins/fingerprint_test.go"]
for path in paths:
    text = path.read_text(encoding="utf-8")
    if "plugins/analysis" in path.as_posix():
        text = text.replace("FFmpegSHA256", "DecoderSHA256").replace("FFmpegVersion", "DecoderVersion")
    else:
        text = text.replace("FFmpegSHA256:", "DecoderSHA256:").replace("FFmpegVersion:", "DecoderVersion:")
    path.write_text(text, encoding="utf-8", newline="\n")

path = root / "plugins/database/sqlite/migrations/README.md"
text = path.read_text(encoding="utf-8")
text = text.replace("当前保留 030—033 四个独立版本，001—029", "当前保留 031—034 四个独立版本，001—030")
text += "\n034 将旧分析器身份的指纹任务及索引原文移入 `retired_fingerprint_evidence` 审计表，撤销旧指纹发布、相关待确认候选和请求幂等记录，新请求重新提取。原歌曲、录音关系、授权、用户及其他任务不变；旧媒体由正常回收处理，备份引用仍受保护。迁移前保留 `before-034` 备份，不在运行代码加入旧 JSON 兼容分支。\n"
path.write_text(text, encoding="utf-8", newline="\n")
