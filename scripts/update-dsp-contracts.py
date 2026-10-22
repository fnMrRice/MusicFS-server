"""追加 DSP 配置契约，并保留已有契约条目的格式。"""
import json
import re
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def obj(properties, required=None):
    return {"type": "object", "additionalProperties": False, "required": list(properties) if required is None else required, "properties": properties}


def ref(name):
    return {"$ref": "#/$defs/" + name}


def array(items, **extra):
    return {"type": "array", "items": items, **extra}


text = {"type": "string"}
number = {"type": "number"}
boolean = {"type": "boolean"}
revision = {"type": "integer", "minimum": 0}
layouts = {"enum": ["mono", "stereo", "5.1", "7.1"]}
outputs = {"enum": ["stereo", "5.1", "7.1"]}
schema = {"const": "dsp-settings-v1"}
definitions = {
    "DSPNodeSpec": obj({"name": {"type": "string", "minLength": 1, "maxLength": 64}, "version": {"const": 1}, "params": {"type": "object", "maxProperties": 64, "propertyNames": {"minLength": 1, "maxLength": 64}, "additionalProperties": number}}, ["name", "version"]),
    "DSPOptions": obj({"chain": array(ref("DSPNodeSpec"), maxItems=15), "outputLayout": outputs, "bufferFrames": {"type": "integer", "minimum": 64, "maximum": 16384}}),
    "DSPFormat": obj({"sampleFormat": {"const": "f32le"}, "sampleRate": {"const": 48000}, "channels": {"enum": [1, 2, 6, 8]}, "layout": layouts}),
    "DSPOutput": obj({"format": ref("DSPFormat"), "canContinueDSP": boolean}),
    "DSPParameter": obj({"key": text, "label": text, "min": number, "max": number, "step": number, "defaultValue": number, "options": array(obj({"label": text, "value": number}))}, ["key", "label", "min", "max", "step", "defaultValue"]),
    "DSPDescriptor": obj({"name": text, "aliases": array(text), "version": {"const": 1}, "displayName": text, "description": text, "input": obj({"sampleFormat": {"const": "f32le"}, "sampleRate": {"const": 48000}, "layouts": array(layouts)}), "output": obj({"canContinueDSP": boolean, "sameAsInput": boolean, "layouts": array(layouts)}), "parameters": array(ref("DSPParameter"))}, ["name", "version", "displayName", "description", "input", "output", "parameters"]),
    "DSPSettingsPage": obj({"scope": {"enum": ["personal", "global"]}, "usesGlobal": boolean, "revision": revision, "schemaRevision": schema, "settings": ref("DSPOptions"), "defaults": ref("DSPOptions"), "catalog": array(ref("DSPDescriptor")), "maxNodes": {"const": 15}, "available": boolean, "unavailableReason": text, "outputLayouts": array(outputs)}, ["scope", "usesGlobal", "revision", "schemaRevision", "settings", "defaults", "catalog", "maxNodes", "available", "outputLayouts"]),
    "DSPSettingsRequest": obj({"expectedRevision": revision, "schemaRevision": schema, "settings": ref("DSPOptions"), "inheritGlobal": boolean}, ["expectedRevision", "schemaRevision"]),
    "DSPGlobalSettingsRequest": obj({"expectedRevision": revision, "schemaRevision": schema, "settings": ref("DSPOptions"), "inheritGlobal": {"const": False}}, ["expectedRevision", "schemaRevision", "settings"]),
    "DSPValidateRequest": obj({"settings": ref("DSPOptions")}),
    "DSPPreview": obj({"input": ref("DSPFormat"), "output": ref("DSPOutput"), "nodes": array(obj({"name": text, "input": ref("DSPFormat"), "output": ref("DSPOutput")}))}),
}
definitions["DSPSettingsRequest"]["oneOf"] = [
    {"required": ["settings"], "properties": {"inheritGlobal": {"const": False}}},
    {"required": ["inheritGlobal"], "properties": {"inheritGlobal": {"const": True}}, "not": {"required": ["settings"]}},
]


def insert_entries(path, key, entries):
    original = path.read_text(encoding="utf-8")
    document = json.loads(original)
    missing = {name: value for name, value in entries.items() if name not in document[key]}
    for name, value in entries.items():
        if name in document[key] and document[key][name] != value:
            raise RuntimeError("Existing contract differs: " + name)
    anchor = '"' + key + '": {'
    addition = "\n" + "\n".join("    " + json.dumps(name) + ": " + json.dumps(value, ensure_ascii=False) + "," for name, value in missing.items())
    if missing:
        updated = original.replace(anchor, anchor + addition, 1)
        json.loads(updated)
        path.write_text(updated, encoding="utf-8", newline="\n")


insert_entries(ROOT / "docs/contracts/http.schema.json", "$defs", definitions)


def response(model):
    return {"200": {"description": "DSP 配置或校验结果", "content": {"application/json": {"schema": {"$ref": "./http.schema.json#/$defs/" + model}}}}, "default": {"$ref": "#/components/responses/Error"}}


def body(model):
    return {"required": True, "content": {"application/json": {"schema": {"$ref": "./http.schema.json#/$defs/" + model}}}}


def route(global_scope=False):
    suffix = "Global" if global_scope else ""
    return {
        "get": {"operationId": "get" + suffix + "DSPSettings", "description": "全局仅管理员可读写。个人未设置或选择继承时使用全局默认，否则个人配置覆盖全局。返回插件声明的输入、输出及参数，不返回可执行路径。", "responses": response("DSPSettingsPage")},
        "put": {"operationId": "update" + suffix + "DSPSettings", "description": "校验整条链和修订后保存，仅影响之后创建的播放会话。输出不可继续DSP时禁止后继节点。bufferFrames 为64至16384；外部MFD1 v1仍最多4096。相同幂等键和原修订可重放最后一次相同请求；其他过期修订返回CONFLICT。全局配置只作默认，不与个人链串联。", "parameters": [{"$ref": "#/components/parameters/CSRF"}, {"$ref": "#/components/parameters/Idempotency"}], "requestBody": body("DSPGlobalSettingsRequest" if global_scope else "DSPSettingsRequest"), "responses": response("DSPSettingsPage")},
    }


paths = {
    "/dsp/settings": route(),
    "/dsp/settings/global": route(True),
    "/dsp/settings/validate": {"post": {"operationId": "validateDSPSettings", "x-idempotency": "read-only", "description": "只读协商节点输入输出，校验声道、终止标记、缓冲区和已探测编码能力，不启动外部插件、不保存设置。", "parameters": [{"$ref": "#/components/parameters/CSRF"}], "requestBody": body("DSPValidateRequest"), "responses": response("DSPPreview")}},
}
insert_entries(ROOT / "docs/contracts/openapi.yaml", "paths", paths)

# 外部 DSP 协议的保留名称与内置节点同步。
for relative in ["configs/server.schema.json", "docs/contracts/plugins/v1/protocol.schema.json"]:
    path = ROOT / relative
    original = path.read_text(encoding="utf-8")
    document = json.loads(original)
    old_names = document["$defs"]["DSPPlugin"]["properties"]["name"]["not"]["enum"]
    new_names = old_names + [name for name in ["channelmix", "musicfs.dsp.channelmix", "musicfs.plugin.decoder.channelmix"] if name not in old_names]
    if new_names != old_names:
        expression = r'"enum"\s*:\s*\[\s*' + r'\s*,\s*'.join(re.escape(json.dumps(name)) for name in old_names) + r'\s*\]'
        updated, count = re.subn(expression, '"enum":' + json.dumps(new_names, separators=(",", ":")), original, count=1)
        if count != 1:
            raise RuntimeError("Reserved DSP names not found: " + relative)
        json.loads(updated)
        path.write_text(updated, encoding="utf-8", newline="\n")

path = ROOT / "docs/contracts/plugins/v1/protocol.schema.json"
original = path.read_text(encoding="utf-8")
document = json.loads(original)
if "bufferFrames" not in document["$defs"]["AudioStart"]["properties"]:
    old = '"dspChain": {"type": "array", "maxItems": 15'
    new = '"bufferFrames": {"type": "integer", "minimum": 64, "maximum": 16384, "default": 4096}, ' + old
    if original.count(old) != 1:
        raise RuntimeError("AudioStart DSP properties not found")
    updated = original.replace(old, new, 1)
    json.loads(updated)
    path.write_text(updated, encoding="utf-8", newline="\n")
print("Updated DSP API contracts")
