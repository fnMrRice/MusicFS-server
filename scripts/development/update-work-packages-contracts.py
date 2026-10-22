"""增量添加作品包合同，保留其他接口和已有格式。"""

import runpy
from pathlib import Path

helpers = runpy.run_path(str(Path(__file__).with_name("update-works-contracts.py")))
obj, text, array, ref = (helpers[key] for key in ("obj", "text", "array", "ref"))
root, sync_entries = helpers["ROOT"], helpers["sync_entries"]
identifier = text(128, 1)
relative = text(4096, 1)
package_kind = {"enum": ["directory", "cd_dump"]}
node = obj({
    "nodeId": text(4096, 1), "parentId": text(4096), "path": relative, "name": text(256, 1),
    "kind": {"enum": ["directory", "audio", "attachment"]},
    "order": {"type": "integer", "minimum": 0, "maximum": 4999},
    "mime": text(256), "sizeBytes": {"type": "integer", "minimum": 0},
    "sourceTrackId": text(128), "assetId": text(128),
    "attachmentKind": {"enum": ["", "image", "text", "download"]},
    "track": {"anyOf": [ref("SourceTrack"), {"type": "null"}]},
})
package = obj({
    "packageId": text(128), "workId": identifier, "providerInstanceId": ref("InstanceID"),
    "rootId": identifier, "relativePath": relative, "name": text(256, 1), "kind": package_kind,
    "revision": {"type": "integer", "minimum": 0}, "importedAt": ref("Timestamp"),
    "nodes": {**array(ref("WorkPackageNode"), 5000), "minItems": 1},
})
location = {"providerInstanceId": ref("InstanceID"), "rootId": identifier, "path": relative}
selection = obj({**location, "kind": package_kind})
definitions = {
    "WorkPackageNode": node, "WorkPackage": package,
    "WorkPackages": obj({"items": array(ref("WorkPackage"), 64)}),
    "WorkPackageSelection": selection,
    "WorkPackageImport": obj({"previewId": identifier}),
    "WorkPackagePreview": obj({
        "previewId": identifier, "workId": identifier, "expiresAt": ref("Timestamp"),
        "package": ref("WorkPackage"), "counts": obj({
            key: {"type": "integer", "minimum": 0, "maximum": 5000}
            for key in ("directories", "audio", "attachments")
        }), "warnings": array(text(2048, 1), 64),
    }),
    "WorkPackageSources": obj({"items": {"type": "array", "items": obj({
        "providerInstanceId": ref("InstanceID"), "name": text(4096, 1),
        "roots": {"type": "array", "items": obj({"id": identifier, "name": text(4096, 1)})},
    })}}),
    "WorkPackageDirectories": obj({"path": relative, "directories": array(obj({"name": text(256, 1), "path": relative}), 1000)}),
}


def operation(name, model, description, request=None, idempotent=False):
    result = {
        "operationId": name, "description": description,
        "responses": {"200": helpers["response"](model, description), "default": {"$ref": "#/components/responses/Error"}},
    }
    if request:
        result["parameters"] = [{"$ref": "#/components/parameters/CSRF"}]
        if idempotent:
            result["parameters"].append({"$ref": "#/components/parameters/Idempotency"})
        else:
            result["x-idempotency"] = "read-only"
        result["requestBody"] = {"required": True, "content": {"application/json": {"schema": {"$ref": f"./http.schema.json#/$defs/{request}"}}}}
    return result


work_id = [{"$ref": "#/components/parameters/ID"}]
browse = operation("browseWorkPackageDirectories", "WorkPackageDirectories", "只浏览本人已授权来源根内相对目录，不返回绝对路径；最多1000个子目录，超限失败。")
browse["parameters"] = [{"name": key, "in": "query", "required": True, "schema": schema} for key, schema in location.items()]
browse["parameters"][0]["schema"] = {"$ref": "./http.schema.json#/$defs/InstanceID"}
paths = {
    "/works/{id}/package-sources": {"parameters": work_id, "get": operation("listWorkPackageSources", "WorkPackageSources", "本人可用的本地来源与已配置根；不接受查询参数。")},
    "/works/{id}/package-directories": {"parameters": work_id, "get": browse},
    "/works/{id}/package-previews": {"parameters": work_id, "post": operation("previewWorkPackage", "WorkPackagePreview", "必须指定副本类型directory或cd_dump；完整有界枚举目录并保留15分钟的本人预览，不发布音轨或作品包。最多5000节点、32层，链接及不完整读取失败。", "WorkPackageSelection")},
    "/works/{id}/packages": {"parameters": work_id,
        "get": operation("listWorkPackages", "WorkPackages", "读取本人作品包及目录树；音轨引用导入版本，附件按节点ID重新授权读取。"),
        "post": operation("importWorkPackage", "WorkPackage", "确认预览、重新校验快照并原子保存音轨与树；同位置更新原包，变化返回409。稳定幂等键支持24小时重放，包括Server重启后的成功确认。", "WorkPackageImport", True)},
    "/works/{id}/packages/{packageId}/attachments/{nodeId}": {
        "parameters": work_id + [{"name": name, "in": "path", "required": True, "schema": identifier} for name in ("packageId", "nodeId")],
        "get": {"operationId": "getWorkPackageAttachment", "description": "鉴权并按导入版本稳定读取原附件；默认仅预览1MiB内UTF-8文本或16MiB内栅格图像。download=1以附件下载，禁止任意路径或URL。",
            "parameters": [{"name": "download", "in": "query", "schema": {"const": "1"}}],
            "responses": {"200": {"description": "原附件内容", "content": {kind: {"schema": {"type": "string", "format": "binary"}} for kind in ("application/octet-stream", "text/plain", "image/png", "image/jpeg", "image/gif")}}, "default": {"$ref": "#/components/responses/Error"}}},
    },
}

if __name__ == "__main__":
    sync_entries(root / "docs/contracts/http.schema.json", "$defs", definitions)
    sync_entries(root / "docs/contracts/openapi.yaml", "paths", paths)
    print("Work package contracts updated")
