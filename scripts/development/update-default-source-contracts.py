"""更新录音默认来源的 HTTP 契约，保留其他定义的格式。"""
import json
from pathlib import Path

root = Path(__file__).resolve().parents[2]
schema_path = root / "docs/contracts/http.schema.json"
text = schema_path.read_text(encoding="utf-8")
schemas = json.loads(text)["$defs"]
nullable_id = {"anyOf": [{"$ref": "#/$defs/ID"}, {"type": "null"}]}
for name in ("SourceTrack", "Recording"):
    schemas[name]["properties"]["manualSourceTrackId"] = nullable_id | {
        "description": "本人手动指定的默认来源；null表示自动。分组顶层sourceTrackId为实际默认。"
    }
schemas["RecordingLinkChange"]["properties"]["operation"]["enum"] = ["link", "unlink", "split", "exclusion", "defaultSource"]
for name in ("SourceTrack", "Recording", "RecordingLinkChange"):
    lines = text.splitlines()
    index = next(i for i, line in enumerate(lines) if line.startswith(f'    "{name}":'))
    lines[index] = f'    "{name}": ' + json.dumps(schemas[name], ensure_ascii=False) + ","
    text = "\n".join(lines) + "\n"
request = {
    "type": "object", "additionalProperties": False,
    "required": ["expectedRevision", "recordingId", "sourceTrackId"],
    "properties": {
        "expectedRevision": {"type": "integer", "minimum": 0},
        "recordingId": {"$ref": "#/$defs/ID"}, "sourceTrackId": nullable_id,
    },
}
if '    "DefaultSourceRequest":' not in text:
    text = text.replace('    "ManualUnlinkRequest":', '    "DefaultSourceRequest": ' + json.dumps(request, ensure_ascii=False) + ',\n    "ManualUnlinkRequest":')
schema_path.write_text(text, encoding="utf-8")

path = root / "docs/contracts/openapi.yaml"
text = path.read_text(encoding="utf-8")
route = {
    "post": {
        "operationId": "setRecordingDefaultSource",
        "description": "指定本人录音的默认来源；sourceTrackId必须为成员或null，null取消手动指定。手动来源不可用时仍保留。自动选择依次按可用性、码率和原文件大小降序。",
        "parameters": [{"$ref": "#/components/parameters/CSRF"}, {"$ref": "#/components/parameters/Idempotency"}],
        "requestBody": {"required": True, "content": {"application/json": {"schema": {"$ref": "./http.schema.json#/$defs/DefaultSourceRequest"}}}},
        "responses": {"200": {"$ref": "#/components/responses/MatchingMutation"}, "default": {"$ref": "#/components/responses/Error"}},
    }
}
if '    "/matching/default-source":' not in text:
    text = text.replace('    "/matching/exclusions":', '    "/matching/default-source": ' + json.dumps(route, ensure_ascii=False) + ',\n    "/matching/exclusions":')
path.write_text(text, encoding="utf-8")
