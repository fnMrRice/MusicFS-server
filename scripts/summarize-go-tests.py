"""从 go test -json 日志提取失败断言，避免输出测试中的大量服务日志。"""
import json
import re
import sys
from collections import defaultdict
from pathlib import Path

outputs = defaultdict(list)
failures = []
for line in Path(sys.argv[1]).read_text(encoding="utf-8-sig", errors="replace").splitlines():
    try:
        event = json.loads(line)
    except json.JSONDecodeError:
        continue
    key = (event.get("Package", ""), event.get("Test", ""))
    if event.get("Action") == "output":
        outputs[key].append(event.get("Output", ""))
    elif event.get("Action") == "fail":
        failures.append(key)
for key in failures:
    if len(sys.argv) > 2 and sys.argv[2] not in key[0]:
        continue
    print("FAIL", *key)
    lines = [line for text in outputs[key] for line in text.splitlines()]
    details = [line for line in lines if re.search(r"\.go:\d+:|^panic:|^# ", line)]
    print("\n".join(details or lines[-3:]))
