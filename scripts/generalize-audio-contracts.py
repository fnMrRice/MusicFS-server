"""将 HTTP 音频身份合同从官方编码器枚举改为中立字段约束。"""

import json
from pathlib import Path


path = Path(__file__).resolve().parents[1] / "docs/contracts/http.schema.json"
source = path.read_text(encoding="utf-8")
document = json.loads(source)
identity = {"type": "string", "minLength": 1, "maxLength": 128,
            "pattern": "^[A-Za-z0-9][A-Za-z0-9._-]*$"}
output = document["$defs"]["OutputProfile"]
output["properties"]["id"] = identity
output["then"] = {"required": ["sampleRate", "channels", "layout"]}
document["$defs"]["TimelineProfile"]["properties"]["outputProfile"] = identity
lines = source.splitlines(keepends=True)
for name in ("OutputProfile", "TimelineProfile"):
    matches = [i for i, line in enumerate(lines) if line.startswith('    "' + name + '":')]
    if len(matches) != 1:
        raise RuntimeError("Expected one complete definition line: " + name)
    value = json.dumps(document["$defs"][name], ensure_ascii=False, separators=(", ", ": "))
    lines[matches[0]] = '    "' + name + '": ' + value + ',\n'
updated = "".join(lines)
json.loads(updated)
path.write_text(updated, encoding="utf-8", newline="")
