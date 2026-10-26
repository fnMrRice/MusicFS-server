"""同步曲目选择与批量操作的 HTTP 合同，保留已有单行定义格式。"""
import json
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def ref(name):
    return {"$ref": f"#/$defs/{name}"}


def obj(properties, required=None):
    return {"type": "object", "required": required or list(properties),
            "properties": properties, "additionalProperties": False}


def update_entries(path, section, entries):
    text = path.read_text(encoding="utf-8")
    lines = text.splitlines()
    for name, value in entries.items():
        prefix = "    " + json.dumps(name, ensure_ascii=False) + ":"
        line = prefix + " " + json.dumps(value, ensure_ascii=False) + ","
        found = next((i for i, existing in enumerate(lines) if existing.startswith(prefix)), None)
        if found is not None:
            lines[found] = line
        else:
            start = lines.index(f'  "{section}": {{')
            lines.insert(start + 1, line)
    result = "\n".join(lines) + "\n"
    json.loads(result)
    path.write_text(result, encoding="utf-8", newline="\n")


ids = {"type": "array", "maxItems": 100000, "uniqueItems": True, "items": ref("ID")}
selection = obj({"batch": {"type": "boolean"}, "sourceTrackIds": ids, "exclude": ids,
                 "providerInstanceId": ref("ID"), "groupBy": {"const": "recording"},
                 "libraryRevision": {"type": "string", "pattern": "^(0|[1-9][0-9]*)$"},
                 "matchingRevision": {"type": "integer", "minimum": 0}}, ["batch"])
selection["allOf"] = [
    {"if": {"properties": {"batch": {"const": True}}},
     "then": {"required": ["libraryRevision"], "properties": {"sourceTrackIds": {"maxItems": 0}}},
     "else": {"required": ["sourceTrackIds"], "properties": {"sourceTrackIds": {"minItems": 1}, "exclude": {"maxItems": 0}}}},
    {"if": {"required": ["groupBy"]}, "then": {"required": ["matchingRevision"]}},
]
selection["description"] = "选择当前账号普通音乐列表的代表行；batch全选同一来源和分组范围内全部行，exclude排除整行。批量绑定libraryRevision，分组绑定matchingRevision；修订变化409，越权或范围外ID为404。"
schema_path = ROOT / "docs/contracts/http.schema.json"
source = json.loads(schema_path.read_text(encoding="utf-8"))["$defs"]["SourceTrack"]["properties"]
fields = ["sourceTrackId", "providerInstanceId", "displayName", "title", "artists", "durationMs", "availability", "assetId", "coverObjectId"]
compact = obj({field: source[field] for field in fields}, fields[:-1])
matching = json.loads(schema_path.read_text(encoding="utf-8"))["$defs"]["ManualLinkRequest"]
matching["required"] = ["expectedRevision"]
matching["properties"]["selection"] = ref("TrackSelection")
matching["oneOf"] = [{"required": ["sourceTrackIds"], "not": {"required": ["selection"]}},
                     {"required": ["selection"], "not": {"required": ["sourceTrackIds"]}}]
update_entries(schema_path, "$defs", {
    "TrackSelection": selection,
    "SelectionTrack": compact,
    "TrackSelectionResult": obj({"items": {"type": "array", "items": ref("SelectionTrack")}, "total": {"type": "integer", "minimum": 0}}),
    "FavoritesSelectionRequest": obj({"selection": ref("TrackSelection"), "favorite": {"type": "boolean"}}),
    "FavoritesSelectionResult": obj({"affected": {"type": "integer", "minimum": 0, "maximum": 20000}, "sourceTrackIds": {**ids, "maxItems": 20000}}),
    "CreatePlaylistSelectionRequest": obj({"selection": ref("TrackSelection"), "name": {"type": "string", "minLength": 1, "maxLength": 256}}),
    "AppendPlaylistSelectionRequest": obj({"selection": ref("TrackSelection"), "expectedRevision": {"type": "integer", "minimum": 1}}),
    "PlaylistSelectionResult": obj({"playlistId": ref("ID"), "ruleRevision": {"type": "integer", "minimum": 1}, "count": {"type": "integer", "minimum": 1, "maximum": 10000}}),
    "ManualLinkRequest": matching,
})


def post(operation, request, response, description, status="200", idempotent=True):
    result = {"operationId": operation, "description": description,
              "parameters": [{"$ref": "#/components/parameters/CSRF"}],
              "requestBody": {"required": True, "content": {"application/json": {"schema": {"$ref": f"./http.schema.json#/$defs/{request}"}}}},
              "responses": {status: {"description": "成功", "content": {"application/json": {"schema": {"$ref": f"./http.schema.json#/$defs/{response}"}}}}, "default": {"$ref": "#/components/responses/Error"}}}
    if idempotent:
        result["parameters"].append({"$ref": "#/components/parameters/Idempotency"})
    else:
        result["x-idempotency"] = "natural"
    return {"post": result}


paths = {
    "/source-tracks/selection": post("selectSourceTracks", "TrackSelection", "TrackSelectionResult", "播放队列按选择读取紧凑引用；只在真正需要播放清单时调用，不用于收藏、歌单或关联。整个选择在同一事务内校验来源、账号和修订。", idempotent=False),
    "/me/favorites/batch": post("setFavoritesSelection", "FavoritesSelectionRequest", "FavoritesSelectionResult", "按选择原子收藏或取消收藏；一次最多20000行，先校验收藏总限额，失败不写入。保留原收藏时间，affected为实际变化数，sourceTrackIds为全部所选代表ID，包含无需改变的记录。", idempotent=False),
    "/playlists/selection": post("createPlaylistSelection", "CreatePlaylistSelectionRequest", "PlaylistSelectionResult", "按选择原子新建个人歌单local绑定；只保存每行代表来源，最多10000条，要求幂等键。响应count为本次加入条数，不返回完整绑定。", "201"),
    "/playlists/{id}/selection": post("appendPlaylistSelection", "AppendPlaylistSelectionRequest", "PlaylistSelectionResult", "同一事务内按expectedRevision校验个人歌单并追加local绑定；保留现有绑定，检查绑定及条目上限。要求幂等键，响应count为本次加入条数。"),
}
paths["/playlists/{id}/selection"]["parameters"] = [{"$ref": "#/components/parameters/ID"}]
paths["/source-tracks/selection"]["post"]["x-idempotency"] = "read-only"
openapi_path = ROOT / "docs/contracts/openapi.yaml"
link = json.loads(openapi_path.read_text(encoding="utf-8"))["paths"]["/matching/links"]
selection_description = " 可改用selection，与sourceTrackIds互斥；同一事务内展开所选分组行全部版本，仍受256来源上限、排除决策和原幂等规则约束。"
if selection_description not in link["post"]["description"]:
    link["post"]["description"] += selection_description
paths["/matching/links"] = link
update_entries(openapi_path, "paths", paths)
