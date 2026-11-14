"""增量维护作品合同，保留其他接口的原有格式及未提交修改。"""

import json
import re
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]


def ref(name):
    return {"$ref": f"#/$defs/{name}"}


def text(maximum, minimum=0):
    return {"type": "string", "minLength": minimum, "maxLength": maximum}


def obj(properties):
    return {"type": "object", "additionalProperties": False,
            "required": list(properties), "properties": properties}


def array(items, maximum, unique=False):
    value = {"type": "array", "items": items, "maxItems": maximum}
    if unique:
        value["uniqueItems"] = True
    return value


identifier = {"type": "string", "pattern": "^[a-z][a-z0-9_-]{0,63}$"}
named = obj({"id": identifier, "name": text(128, 1)})
descriptor = obj({
    "id": identifier, "pluginId": text(128, 1), "pluginVersion": text(128, 1),
    "name": text(128, 1), "description": text(2048),
    "creditRoles": array(named, 64, True),
    "externalIdNamespaces": array(named, 64, True),
    "views": {"type": "array", "minItems": 1, "maxItems": 2,
              "uniqueItems": True, "items": {"enum": ["work-list", "work-detail"]}},
})
descriptor["properties"]["library"] = obj({
    "order": {"type": "integer", "minimum": 0},
    "icon": identifier,
    "trackListOptional": {"type": "boolean"},
})
fields = {
    "title": text(256, 1), "description": text(16384),
    "externalIds": array(obj({"namespace": identifier, "value": text(256, 1)}), 32, True),
    "credits": array(obj({"role": identifier, "name": text(256, 1)}), 128, True),
    "tags": array(text(64, 1), 64, True),
}
definitions = {
    "ContentType": descriptor,
    "ContentTypes": obj({"items": {"type": "array", "items": ref("ContentType")}}),
    "CreateWork": obj({"contentType": identifier, **fields}),
    "UpdateWork": obj({"expectedRevision": {"type": "integer", "minimum": 1,
                                             "maximum": 9223372036854775806}, **fields}),
    "Work": obj({"workId": text(128, 1), "contentType": identifier, **fields,
                 "showInTrackList": {"type": "boolean"},
                 "revision": {"type": "integer", "minimum": 1},
                 "createdAt": ref("Timestamp"), "updatedAt": ref("Timestamp")}),
    "Works": obj({"items": array(ref("Work"), 100),
                  "nextAfter": {"type": ["string", "null"], "minLength": 1, "maxLength": 128}}),
}
for request in ("CreateWork", "UpdateWork"):
    definitions[request]["properties"]["showInTrackList"] = {
        "type": "boolean", "default": False,
        "description": "将本作品的音轨纳入普通曲目列表。只有声明 trackListOptional 的类型可以开启。",
    }


def response(model, description):
    return {"description": description, "content": {"application/json": {
        "schema": {"$ref": f"./http.schema.json#/$defs/{model}"}}}}


def operation(operation_id, model, description, request=None):
    result = {"operationId": operation_id, "description": description,
              "responses": {"201" if operation_id == "createWork" else "200":
                            response(model, description),
                            "default": {"$ref": "#/components/responses/Error"}}}
    if request:
        result["parameters"] = [{"$ref": "#/components/parameters/CSRF"},
                                {"$ref": "#/components/parameters/Idempotency"}]
        result["requestBody"] = {"required": True, "content": {"application/json": {
            "schema": {"$ref": f"./http.schema.json#/$defs/{request}"}}}}
    return result


list_works = operation("listWorks", "Works", "本人作品资料，按稳定编号升序分页；查询条件取交集，非固定快照。")
list_works["parameters"] = [
    {"name": name, "in": "query", "schema": schema, "description": description}
    for name, schema, description in [
        ("contentType", identifier, "精确内容类型，可查询已停用类型的资料"),
        ("packageKind", {"enum": ["directory", "cd_dump"]}, "至少有一个指定副本类型的作品，与其他条件取交集；多个包不重复作品"),
        ("q", text(256, 1), "标题、简介、外部编号、署名的字面子串；ASCII 大小写不敏感"),
        ("tag", text(64, 1), "精确标签"), ("credit", text(256, 1), "精确参与者名称"),
        ("after", text(128, 1), "上一页 nextAfter；改变筛选条件应重新从首屏读取"),
        ("limit", {"type": "integer", "minimum": 1, "maximum": 100, "default": 50}, "页面大小"),
    ]
]
paths = {
    "/content-types": {"get": operation("listContentTypes", "ContentTypes", "已注册的内容类型。library 声明作品库入口顺序、图标及可选的曲目列表展示。需要登录。")},
    "/works": {"get": list_works,
               "post": operation("createWork", "Work", "按已注册类型创建本人作品资料；不扫描文件、不按编号自动合并。", "CreateWork")},
    "/works/{id}": {"parameters": [{"$ref": "#/components/parameters/ID"}],
                    "get": operation("getWork", "Work", "读取本人作品资料；无需内容插件仍在注册表中。"),
                    "put": operation("updateWork", "Work", "按预期修订完整替换资料；类型不可变，旧修订返回409，同键同参数重放。", "UpdateWork")},
}


def add_entries(path, section, additions):
    source = path.read_text(encoding="utf-8")
    document = json.loads(source)
    existing = document[section]
    for key, value in additions.items():
        if key in existing and existing[key] != value:
            raise ValueError(f"Existing contract differs: {key}")
    missing = {key: value for key, value in additions.items() if key not in existing}
    if not missing:
        return
    match = re.search(r'"' + re.escape(section) + r'"\s*:\s*\{', source)
    if not match:
        raise ValueError(f"Missing section: {section}")
    insertion = "\n" + "".join(
        "    " + json.dumps(key) + ": " + json.dumps(value, ensure_ascii=False) + ",\n"
        for key, value in missing.items()
    )
    result = source[:match.end()] + insertion + source[match.end():]
    json.loads(result)
    path.write_text(result, encoding="utf-8", newline="\n")


def sync_entries(path, section, additions):
    """仅替换本脚本维护的单行定义，其余契约格式与内容保持原样。"""
    source = path.read_text(encoding="utf-8")
    existing = json.loads(source)[section]
    for key, value in additions.items():
        if key not in existing or existing[key] == value:
            continue
        pattern = r"^    " + re.escape(json.dumps(key)) + r": .*?,?$"
        line = "    " + json.dumps(key) + ": " + json.dumps(value, ensure_ascii=False) + ","
        source, count = re.subn(pattern, lambda match: line, source, flags=re.M)
        if count != 1:
            raise ValueError(f"Expected a single owned contract line: {key}")
    json.loads(source)
    path.write_text(source, encoding="utf-8", newline="\n")
    add_entries(path, section, additions)


if __name__ == "__main__":
    sync_entries(ROOT / "docs/contracts/http.schema.json", "$defs", definitions)
    sync_entries(ROOT / "docs/contracts/openapi.yaml", "paths", paths)
    print("Work contracts updated")
