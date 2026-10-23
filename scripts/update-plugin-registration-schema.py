"""同步受信任注册能力的配置 Schema，保留其余定义和紧凑格式。"""
import json
from pathlib import Path

path = Path(__file__).resolve().parents[1] / "configs/server.schema.json"
text = path.read_text(encoding="utf-8")
schema = json.loads(text)
definitions = schema["$defs"]
official = definitions["Plugin"]["properties"]["pluginId"]["enum"]
definitions["Plugin"]["properties"]["pluginId"] = {"type": "string", "pattern": "^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$"}
dsp = definitions["DSPPlugin"]
reserved = dsp["properties"]["name"].pop("not")
dsp["properties"]["builtin"]["description"] = "由可信装配注册的进程内 DSP；加载器检查工厂是否存在。"
dsp["if"] = {"anyOf": [{"required": ["builtin"], "properties": {"builtin": {"const": True}}}, {"properties": {"name": {"const": "musicfs.dsp.gain"}}}]}
dsp["else"] = {"required": ["executable", "sha256"], "properties": {"name": {"not": reserved}}}
dsp.pop("allOf")
for name in ["Plugin", "DSPPlugin"]:
    start = text.index('    "' + name + '": ')
    end = text.index("\n", start)
    text = text[:start] + '    "' + name + '": ' + json.dumps(definitions[name], ensure_ascii=False, separators=(",", ":")) + "," + text[end:]
registered = {"allOf": [{"$ref": "#/$defs/Plugin"}], "description": "其他受信任注册插件；角色、能力、实例数量和配置由运行时注册声明校验，未注册身份仍拒绝。", "properties": {"pluginId": {"not": {"enum": official}}, "instances": {"items": {"$ref": "#/$defs/Instance"}}}}
marker = '    "OnlinePlugin": '
text = text.replace(marker, '    "RegisteredPlugin": ' + json.dumps(registered, ensure_ascii=False, separators=(",", ":")) + ',\n' + marker, 1)
text = text.replace('{"$ref": "#/$defs/OnlinePlugin"}]', '{"$ref": "#/$defs/OnlinePlugin"}, {"$ref": "#/$defs/RegisteredPlugin"}]', 1)
rules = schema["allOf"][:1]
rules[0]["then"]["properties"]["plugins"]["contains"]["properties"].pop("pluginId")
start = text.index('\n  "allOf": [')
end = text.index('\n  "$defs":', start)
rendered = json.dumps(rules, ensure_ascii=False, indent=2).replace("\n", "\n  ")
text = text[:start] + '\n  "allOf": ' + rendered + "," + text[end:]
text = text.replace("非空链要求已启用的 FFmpeg 实例", "非空链要求已启用且声明音频处理能力的实例")
text = text.replace("默认禁用的显式本地FLAC写回；不自动授权所有音乐根，不接受路径或原始正文。启用时加载器还校验每个Local Files实例与根的双层启用状态。", "默认禁用的显式标签写回，具体格式由标签插件声明；不自动授权所有音乐根，不接受路径或原始正文。启用时加载器校验目录型来源实例与根的双层启用状态。")
text = text.replace("手动本实例音频指纹；固定 Chromaprint 1.6.1/算法 2，禁用时不启动工具，无自动上传或外部检索。", "手动本实例音频指纹；默认装配 Chromaprint 1.6.1/算法 2，算法身份由分析插件提供，禁用时不启动工具，无自动上传或外部检索。")
json.loads(text)
path.write_text(text, encoding="utf-8", newline="\n")
