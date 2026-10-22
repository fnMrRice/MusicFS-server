"""同步按源音频码率选择的播放输出格式契约，保留其他契约的排版与变更。"""

import json
from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]
MP3_RATES = [32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320]
OPUS_RANGES = {
    "5.1": r"([5-9][0-9]{2}|[1-9][0-9]{3,5}|1[0-4][0-9]{5}|15[0-2][0-9]{4}|153[0-5][0-9]{3}|1536000)",
    "7.1": r"([5-9][0-9]{2}|[1-9][0-9]{3,5}|1[0-9]{6}|20[0-3][0-9]{4}|204[0-7][0-9]{3}|2048000)",
}


def compact(value):
    return json.dumps(value, ensure_ascii=False, separators=(", ", ": "))


def identifier(layout):
    default = 256000 if layout == "5.1" else 384000
    return {"type": "string", "pattern": "^opus-" + layout.replace(".", r"\.") + "-" + OPUS_RANGES[layout] + "$", "not": {"const": f"opus-{layout}-{default}"}}


def definitions():
    cases = []
    for layout, channels, prefix in [("stereo", 2, "mp3-"), ("mono", 1, "mp3-mono-")]:
        for rate in MP3_RATES:
            cases.append({"properties": {name: {"const": value} for name, value in {
                "id": f"{prefix}{rate}", "mime": "audio/mpeg", "codec": "mp3", "container": "mp3", "channels": channels, "layout": layout, "bitrateBps": rate * 1000,
            }.items()}})
    for layout, channels, default in [("5.1", 6, 256000), ("7.1", 8, 384000)]:
        common = {name: {"const": value} for name, value in {
            "mime": "audio/webm", "codec": "opus", "container": "webm", "channels": channels, "layout": layout,
        }.items()}
        cases.append({"properties": {**common, "id": {"const": f"opus-{layout}"}, "bitrateBps": {"const": default}}})
        cases.append({"properties": {**common, "id": identifier(layout), "bitrateBps": {"type": "integer", "minimum": 500, "maximum": 256000 * channels, "not": {"const": default}}}})
    return cases


def replace_definition(text, name, definition):
    start = text.index(f'    "{name}": ')
    value_start = start + len(f'    "{name}": ')
    _, length = json.JSONDecoder().raw_decode(text[value_start:])
    return text[:value_start] + compact(definition) + text[value_start + length:]


def main():
    ids = {"anyOf": [{"enum": [f"{prefix}{rate}" for prefix in ["mp3-", "mp3-mono-"] for rate in MP3_RATES] + ["opus-5.1", "opus-7.1"]}, identifier("5.1"), identifier("7.1")]}
    for relative, plugin in [("docs/contracts/http.schema.json", False), ("docs/contracts/plugins/v1/protocol.schema.json", True)]:
        path = ROOT / relative
        text = path.read_text(encoding="utf-8")
        data = json.loads(text)
        defs = data["$defs"]
        if plugin:
            profile = defs["AudioProfile"]
            profile["oneOf"] = definitions()
            text = replace_definition(text, "AudioProfile", profile)
            for name in ["AudioPlan", "AudioPrepared"]:
                definition = defs[name]
                definition["properties"]["profile"] = ids
                text = replace_definition(text, name, definition)
        else:
            profile = defs["OutputProfile"]
            props = profile["properties"]
            props["id"] = ids
            props["sampleRate"]["maximum"] = 1048575
            props["channels"] = {"type": "integer", "minimum": 1, "maximum": 8}
            props["layout"] = {"type": "string", "minLength": 1, "maxLength": 128}
            props["bitrateBps"] = {"type": "integer", "minimum": 1}
            profile["then"]["oneOf"] = definitions()
            text = replace_definition(text, "OutputProfile", profile)
            timeline = defs["TimelineProfile"]
            timeline["properties"]["outputProfile"] = ids
            text = replace_definition(text, "TimelineProfile", timeline)
        path.write_text(text, encoding="utf-8", newline="\n")


if __name__ == "__main__":
    main()
