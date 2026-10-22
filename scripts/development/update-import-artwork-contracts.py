"""同步扫描封面导入声明，保留其他合同定义与格式。"""

import json
import re
from pathlib import Path

root = Path(__file__).resolve().parents[2]


def replace_definition(source, name, value):
    match = re.search(r'^    "' + re.escape(name) + r'":\s*', source, re.M)
    if match is None:
        raise ValueError(f"Missing contract definition: {name}")
    _, length = json.JSONDecoder().raw_decode(source[match.end():])
    return source[:match.end()] + json.dumps(value, ensure_ascii=False) + source[match.end() + length:]


for relative in ("docs/contracts/http.schema.json", "docs/contracts/plugins/v1/protocol.schema.json", "configs/server.schema.json"):
    path = root / relative
    source = path.read_text(encoding="utf-8")
    definitions = json.loads(source)["$defs"]
    for name in ("ImportOptions", "ImportOverrides"):
        if name not in definitions:
            continue
        model = definitions[name]
        model["properties"]["importArtwork"] = {
            "type": "boolean",
            "description": "扫描成功后提取内嵌或同名旁置封面并保存媒体数据目录；只补缺失且未锁定的封面。Local Files 默认开启，可按目录或本次扫描关闭。",
        }
        source = replace_definition(source, name, model)
    if "MetadataAutomationResult" in definitions:
        model = definitions["MetadataAutomationResult"]
        model["properties"]["reason"] = {"enum": ["importLyrics", "importArtwork", "automatic"]}
        source = replace_definition(source, "MetadataAutomationResult", model)
    json.loads(source)
    path.write_text(source, encoding="utf-8", newline="\n")
print("Scan artwork import contracts updated")
