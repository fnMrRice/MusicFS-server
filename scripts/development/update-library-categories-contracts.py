"""同步曲目分类与封面合同，只替换本功能维护的定义。"""

import copy
import json
import runpy
from pathlib import Path

helpers = runpy.run_path(str(Path(__file__).with_name("update-works-contracts.py")))
root, sync = helpers["ROOT"], helpers["sync_entries"]
schema_path = root / "docs/contracts/http.schema.json"
api_path = root / "docs/contracts/openapi.yaml"
definitions = json.loads(schema_path.read_text(encoding="utf-8"))["$defs"]
paths = json.loads(api_path.read_text(encoding="utf-8"))["paths"]


def text(limit=128):
    return {"type": "string", "maxLength": limit}


def ref(name):
    return {"$ref": f"#/$defs/{name}"}


def obj(properties):
    return {"type": "object", "additionalProperties": False,
            "required": list(properties), "properties": properties}


kind = {"enum": ["artist", "album", "genre", "folder"]}
category_ref = obj({"categoryId": ref("ID"), "name": text(4096)})
category = obj({
    "categoryId": ref("ID"), "kind": kind, "name": text(4096),
    "unknown": {"type": "boolean"}, "providerInstanceId": text(),
    "rootId": text(), "parentId": {"anyOf": [ref("ID"), {"type": "null"}]},
    "trackCount": {"type": "integer", "minimum": 0},
    "breadcrumbs": {"type": "array", "items": ref("CatalogCategoryRef")},
})
query = copy.deepcopy(definitions["CatalogQuery"])
query["properties"]["type"] = {"enum": ["tracks", "albums", "artists", "categories"]}
query["properties"].update({"categoryKind": kind, "categoryId": text(), "parentId": text()})
# 查询响应未指定来源时返回空串，输入仍须是合法实例或空范围。
query["properties"]["providerInstanceId"] = {"anyOf": [ref("InstanceID"), {"const": ""}]}
query["allOf"] = [
    {"if": {"required": ["type"], "properties": {"type": {"const": "categories"}}},
     "then": {"required": ["categoryKind"]}},
    {"if": {"required": ["categoryId"]}, "then": {"required": ["categoryKind"]}},
    {"if": {"required": ["categoryKind"]}, "then": {
        "properties": {"type": {"enum": ["tracks", "categories"]}},
        "if": {"not": {"required": ["type"], "properties": {"type": {"const": "categories"}}}},
        "then": {"required": ["categoryId"]}}},
    {"if": {"required": ["parentId"]}, "then": {
        "required": ["type", "categoryKind"],
        "properties": {"type": {"const": "categories"}, "categoryKind": {"const": "folder"}},
        "not": {"required": ["categoryId"]}}},
]
page = copy.deepcopy(definitions["CatalogPage"])
variants = page["properties"]["items"]["items"]["oneOf"]
if ref("CatalogCategory") not in variants:
    variants.append(ref("CatalogCategory"))
track = copy.deepcopy(definitions["SourceTrack"])
track["properties"]["coverObjectId"] = {"anyOf": [ref("ID"), {"type": "null"}]}
sync(schema_path, "$defs", {
    "CatalogCategoryRef": category_ref, "CatalogCategory": category,
    "CatalogQuery": query, "CatalogPage": page, "SourceTrack": track,
})

search = copy.deepcopy(paths["/library/search"])
operation = search["get"]
operation["description"] = (
    "普通音乐范围包含已开启曲目展示的CD专辑，排除其余CD专辑、ASMR与电台音频；分类及计数只含可见成员。"
    "当前用户完整曲库按tracks/albums/artists/categories查询；categories要求categoryKind，"
    "tracks以categoryKind和categoryId进入分类。分类按来源曲目计数，专辑标签分类不创建SourceAlbum。"
    "多选参数重复同名键，条件取交集；10分钟固定快照，游标绑定全部规范化条件，失效410。"
    "availableOnly仅表示扫描观测状态。曲目coverObjectId仅引用当前有效封面，"
    "已合并曲目顶层采用录音人工偏好或最高像素成员封面，versions保留来源原图。"
)
parameters = operation["parameters"]
parameters[:] = [p for p in parameters if p["name"] not in ("categoryKind", "categoryId", "parentId")]
for parameter in parameters:
    if parameter["name"] == "type":
        parameter["schema"] = query["properties"]["type"]
parameters[1:1] = [
    {"name": name, "in": "query", "schema": model, "description": description}
    for name, model, description in [
        ("categoryKind", kind, "分类类型"),
        ("categoryId", text(), "不透明分类编号；集合查询时读取该分类，曲目查询时限定成员"),
        ("parentId", text(), "仅文件夹集合可用；省略返回根，设置返回直接子目录，与categoryId互斥"),
    ]
]
categories = copy.deepcopy(search)
categories["get"]["operationId"] = "listLibraryCategories"
categories["get"]["description"] = (
    "普通音乐范围包含已开启曲目展示的CD专辑，排除其余CD专辑、ASMR与电台音频；分类及计数只含可见成员。"
    "四类曲目分类固定快照。歌手和专辑按来源及标签分组，流派跨来源折叠大小写；"
    "未知元数据具有独立身份。文件夹只返回扫描范围内的安全目录名称和不透明编号；"
    "trackCount统计满足筛选的来源曲目，目录计数包含后代。breadcrumbs从根到自身。"
    "用categoryId读取单分类，不存在或不属于当前用户/来源返回404。"
)
categories["get"]["parameters"] = [
    p for p in categories["get"]["parameters"] if p["name"] != "type"
]
for parameter in categories["get"]["parameters"]:
    if parameter["name"] == "categoryKind":
        parameter["required"] = True
    if parameter["name"] == "sort":
        parameter["schema"] = {"enum": ["name"]}
sync(api_path, "paths", {"/library/search": search, "/library/categories": categories})
print("Library category and cover contracts updated")
