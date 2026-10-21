"""更新主进程内置插件的配置与管理协议，保留其他 Schema 的排版。"""

import json
import re
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def replace_object(text, name, value):
    match = re.search(r'^    "' + re.escape(name) + r'":\s*', text, re.M)
    if match is None:
        raise ValueError(f"Missing schema: {name}")
    _, length = json.JSONDecoder().raw_decode(text[match.end():])
    encoded = json.dumps(value, ensure_ascii=False, separators=(",", ":"))
    return text[:match.end()] + encoded + text[match.end() + length:]


path = ROOT / "configs/server.schema.json"
text = path.read_text(encoding="utf-8")
schema = json.loads(text)
mode = {"enum": ["builtin", "process"], "default": "builtin"}
storage = schema["properties"]["storage"]
storage["properties"]["execution"] = mode
storage["description"] = "默认内置 SQLite 在 Server 主进程运行；旧官方 SQLite Worker 配置自动迁移。显式外部后端使用进程模式，数据库不能禁用。"


def program_pattern(name):
    # 与运行时文件名识别一致，兼容大小写和两个平台的路径分隔符。
    letters = "".join("[" + c.lower() + c.upper() + "]" if c.isalpha() else re.escape(c) for c in name)
    return r"(^|[/\\])" + letters + r"(\.[eE][xX][eE])?$"


official_sqlite = [{"properties": {"executable": {"pattern": program_pattern(name)}}}
                   for name in ("musicfs-plugin-storage-sqlite", "musicfs-plugin-sqlite")]
official_sqlite.append({
    "required": ["args"],
    "properties": {
        "executable": {"pattern": program_pattern("musicfs-plugins")},
        "args": {"enum": [["--plugin", "storage-sqlite"], ["--plugin=storage-sqlite"]]},
    },
})
storage["allOf"] = [
    {
        "if": {"required": ["execution"], "properties": {"execution": {"const": "process"}}},
        "then": {"required": ["backend"], "properties": {"backend": {"const": "plugin"}}},
    },
    {
        "if": {"required": ["execution", "backend"], "properties": {
            "execution": {"const": "builtin"}, "backend": {"const": "plugin"},
        }},
        # 原配置仍写 plugin 时，只允许运行时能识别并迁移的官方 SQLite 入口。
        "then": {"anyOf": official_sqlite},
    },
]
text = replace_object(text, "storage", storage)
plugin = schema["$defs"]["Plugin"]
plugin["properties"]["execution"] = mode
plugin["if"] = {"allOf": [{"$ref": "#/$defs/Enabled"}, {"required": ["execution"], "properties": {"execution": {"const": "process"}}}]}
text = replace_object(text, "Plugin", plugin)
dsp = schema["$defs"]["DSPPlugin"]
dsp["required"] = ["name"]
dsp["properties"]["builtin"] = {"type": "boolean", "description": "官方 musicfs.dsp.gain 在主进程内执行；其他插件不可声明内置。"}
dsp["if"] = {"properties": {"name": {"const": "musicfs.dsp.gain"}}}
dsp["else"] = {"required": ["executable", "sha256"]}
dsp["allOf"] = [{
    "if": {"required": ["builtin"], "properties": {"builtin": {"const": True}}},
    "then": {"properties": {"name": {"const": "musicfs.dsp.gain"}}},
}]
text = replace_object(text, "DSPPlugin", dsp)
ffmpeg = schema["$defs"]["FFmpegPlugin"]
ffmpeg["if"] = {"allOf": [{"$ref": "#/$defs/Enabled"}, {"not": {"$ref": "#/$defs/BundledPlugin"}}, {"required": ["execution"], "properties": {"execution": {"const": "process"}}}]}
text = replace_object(text, "FFmpegPlugin", ffmpeg)
path.write_text(text, encoding="utf-8")

path = ROOT / "docs/contracts/openapi.yaml"
text = path.read_text(encoding="utf-8")
text = text.replace(
    "包含普通插件、必需数据库及官方 DSP 增益；",
    "包含普通插件、必需数据库、官方 DSP 增益及音频分析、标签格式、匹配评分、内容转换四类内置算法；execution=builtin 表示在 Server 主进程运行，process 表示显式外部插件。固定内置算法 stateMutable=false 且无普通实例，其 builtin 字段仅描述业务配置与能力，不表示工具就绪或任务正在运行；",
)
path.write_text(text, encoding="utf-8")

path = ROOT / "docs/contracts/http.schema.json"
text = path.read_text(encoding="utf-8")
schema = json.loads(text)
managed = schema["$defs"]["ManagedPlugin"]
managed["properties"]["execution"] = {"enum": ["builtin", "process"], "description": "builtin 在 Server 主进程内执行，process 为显式外部插件。"}
managed["properties"]["builtin"] = {
    "type": "object", "additionalProperties": False,
    "required": ["configured", "capabilities"],
    "description": "固定内置算法的装配和业务配置，不表示工具依赖就绪或当前任务正在运行。",
    "properties": {"configured": {"type": "boolean"}, "capabilities": {"type": "array", "maxItems": 16, "items": {"type": "string", "maxLength": 128}}},
}
managed["properties"]["stateMutable"]["description"] = "是否允许普通插件启停；数据库、DSP 和固定内置算法为 false。"
text = replace_object(text, "ManagedPlugin", managed)
path.write_text(text, encoding="utf-8")
