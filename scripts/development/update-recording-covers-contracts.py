"""同步合并录音封面合同，保留其他功能的定义和格式。"""

import copy
import json
import runpy
from pathlib import Path

helpers = runpy.run_path(str(Path(__file__).with_name("update-works-contracts.py")))
root, sync = helpers["ROOT"], helpers["sync_entries"]
ref, obj = helpers["ref"], helpers["obj"]
schema_path = root / "docs/contracts/http.schema.json"
definitions = json.loads(schema_path.read_text(encoding="utf-8"))["$defs"]
nullable_id = {"anyOf": [ref("ID"), {"type": "null"}]}
dimension = {"type": "integer", "minimum": 0, "maximum": 8192}
candidate = obj({
    "objectId": ref("ID"), "sourceTrackId": ref("ID"),
    "providerInstanceId": ref("InstanceID"), "width": dimension, "height": dimension,
})
candidate["description"] = "当前成员已选用且仍有效的封面；宽高来自服务端解码，0表示历史对象尺寸未知。"
cover = obj({
    "recordingId": ref("ID"), "revision": {"type": "integer", "minimum": 0},
    "coverObjectId": nullable_id, "manualObjectId": nullable_id,
    "candidates": {"type": "array", "maxItems": 256, "items": ref("RecordingCoverCandidate")},
})
selection = obj({
    "expectedRevision": {"type": "integer", "minimum": 0, "maximum": 9223372036854775806},
    "objectId": nullable_id,
})
content = copy.deepcopy(definitions["ContentObject"])
for field in ("width", "height"):
    content["properties"][field] = {"type": "integer", "minimum": 1, "maximum": 8192,
                                   "description": "服务端验证图片正文后取得的像素尺寸；历史未知或非图片对象省略。"}
sync(schema_path, "$defs", {
    "RecordingCoverCandidate": candidate, "RecordingCover": cover,
    "SelectRecordingCover": selection, "ContentObject": content,
})
operation = helpers["operation"]
sync(root / "docs/contracts/openapi.yaml", "paths", {
    "/recordings/{id}/cover": {
        "parameters": [{"$ref": "#/components/parameters/ID"}],
        "get": operation("getRecordingCover", "RecordingCover",
                         "读取本人录音的有效封面及当前成员候选。人工偏好优先，否则按像素面积、短边降序及对象编号升序选取；未知尺寸排末。失效人工偏好保留，展示回退自动结果。"),
        "put": operation("selectRecordingCover", "RecordingCover",
                         "按独立封面修订保存人工偏好，objectId必须显式提供，null恢复自动。只能选当前成员有效候选；失效返回RESOURCE_CHANGED，过期修订或幂等键冲突返回CONFLICT。此操作不改来源封面绑定、匹配关系或播放来源。",
                         "SelectRecordingCover"),
    },
})
print("Recording cover contracts updated")
