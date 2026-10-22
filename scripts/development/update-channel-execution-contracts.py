"""同步禁用服务端声道插件的配置与内部 Worker 字段，保留其他内容排版。"""

import json
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]


def replace_value(text, marker, value):
    start = text.index(marker) + len(marker)
    _, length = json.JSONDecoder().raw_decode(text[start:])
    return text[:start] + json.dumps(value, ensure_ascii=False) + text[start + length:]


path = ROOT / "configs/server.schema.json"
text = path.read_text(encoding="utf-8")
config = json.loads(text)
channels = config["properties"]["audioChannels"]
channels["properties"]["enabled"] = {"type": "boolean", "default": False}
channels["description"] = "声道扩展插件默认禁用；仅显式启用时应用 outputLayout，修改后重启生效。禁用时编码器按来源声道选择兼容格式。"
text = replace_value(text, '    "audioChannels": ', channels)
condition = config["allOf"][0]["if"]
condition["properties"]["audioChannels"]["required"] = ["enabled", "outputLayout"]
condition["properties"]["audioChannels"]["properties"]["enabled"] = {"const": True}
head, tail = text.split('\n  "allOf": [', 1)
tail = replace_value(tail, '      "if": ', condition)
text = head + '\n  "allOf": [' + tail
path.write_text(text, encoding="utf-8", newline="\n")

path = ROOT / "docs/contracts/plugins/v1/protocol.schema.json"
text = path.read_text(encoding="utf-8")
start = json.loads(text)["$defs"]["AudioStart"]
start["properties"]["disableChannelMix"] = {"type": "boolean", "default": False, "description": "禁止声道插件及其历史节点；宿主按来源选择输出声道，仅保留编码兼容所需的 FFmpeg 格式适配。"}
start["description"] = "宿主先选择实际编码档案。disableEffects 禁止全部音效，disableChannelMix 禁止声道插件并由 FFmpeg 完成必要编码兼容适配；未禁用时按配置执行声道转换与 DSP。有状态效果定位重建曲首历史后裁切；外部 MFD1 v1 节点只支持双声道。PCM 不进入 JSON-RPC。"
text = replace_value(text, '    "AudioStart": ', start)
path.write_text(text, encoding="utf-8", newline="\n")
