"""维护普通音乐浏览范围及显式来源读取契约，保留其他接口的修改。"""

import copy
import json
import runpy
from pathlib import Path


helpers = runpy.run_path(str(Path(__file__).with_name("update-works-contracts.py")))
root, sync = helpers["ROOT"], helpers["sync_entries"]
schema_path = root / "docs/contracts/http.schema.json"
query = copy.deepcopy(json.loads(schema_path.read_text(encoding="utf-8"))["$defs"]["CatalogQuery"])
# 未限定来源的规范化查询会返回空字符串，响应合同需保留这个已存在的状态。
query["properties"]["providerInstanceId"] = {"anyOf": [{"$ref": "#/$defs/InstanceID"}, {"const": ""}]}
sync(schema_path, "$defs", {"CatalogQuery": query})
api_path = root / "docs/contracts/openapi.yaml"
paths = json.loads(api_path.read_text(encoding="utf-8"))["paths"]
scope = "普通音乐范围排除本人已发布ASMR作品或CD抓轨副本的音频，分类及计数只含可见成员。"
updates = {}
for route, method in (
    ("/source-tracks", "get"), ("/source-albums", "get"),
    ("/library/search", "get"),
    ("/library/facets", "get"), ("/library/queries", "post"),
):
    value = copy.deepcopy(paths[route])
    description = value[method].get("description", "")
    if route == "/source-tracks":
        description = description.replace("含完整versions", "含可见音乐versions").replace(
            "保留组内所有本人来源", "保留组内所有可见音乐来源")
    if scope not in description:
        description = scope + description
    value[method]["description"] = description
    updates[route] = value

updates["/source-tracks/{id}"] = {
    "parameters": [{"$ref": "#/components/parameters/ID"}],
    "get": {
        "operationId": "getSourceTrack",
        "description": (
            "按显式来源编号读取本人SourceTrack，供歌单和作品引用解析；包含ASMR及CD抓轨音频。"
            "不存在或属于其他用户返回404。不授予播放权限，不公开物理路径。"
        ),
        "responses": {
            "200": helpers["response"]("SourceTrack", "本人来源音轨"),
            "default": {"$ref": "#/components/responses/Error"},
        },
    },
}
sync(api_path, "paths", updates)
print("Library scope contracts updated")
