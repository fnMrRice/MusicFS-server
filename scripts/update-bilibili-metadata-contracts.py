"""按定义更新 Bilibili 元数据契约，保留其他任务的内容与文件布局。"""

import json
import re
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
NUM = r"[1-9][0-9]{0,19}"
VIDEO = rf"video:BV[0-9A-Za-z]{{10}}:{NUM}"
AUDIO = rf"audio:{NUM}"
MISSING = rf"unavailable:[0-9]{{1,5}}:{NUM}"
TRACK = rf"^(?:[A-Za-z0-9]{{1,64}}|{VIDEO}|{AUDIO})$"
ENTRY = rf"^(?:[A-Za-z0-9]{{1,64}}|{VIDEO}|{AUDIO}|{MISSING})$"
PLAYLIST = rf"^(?:{NUM}|(?:favorite|audio-menu):{NUM})$"
REFERENCE = rf"^(?:[A-Za-z0-9]{{1,64}}|{VIDEO}|{AUDIO}|{MISSING}|(?:favorite|audio-menu):{NUM})$"
CURSOR = r"^(?:[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+|[0-9a-f]{48}:[1-9][0-9]{0,4})$"
TRACK_URL = rf"^https://(?:music\.163\.com/song\?id={NUM}|y\.qq\.com/n/ryqq/songDetail/[A-Za-z0-9]{{1,64}}|www\.bilibili\.com/(?:video/BV[0-9A-Za-z]{{10}}\?cid={NUM}|audio/au{NUM}))$"
PLAYLIST_URL = rf"^https://(?:music\.163\.com/playlist\?id={NUM}|www\.bilibili\.com/(?:medialist/detail/ml{NUM}|audio/am{NUM}))$"
BILI_CAPS = ["search.tracks", "search.playlists", "metadata.track", "track.details", "metadata.tracks.batch", "playlist.tracks", "playlist.observe", "stream.resolve"]


def load(relative):
    return json.loads((ROOT / relative).read_bytes().decode("utf-8-sig"))


def replace_lines(relative, changes, compact=False):
    path = ROOT / relative
    raw = path.read_bytes().decode("utf-8")
    seen = set()
    lines = raw.splitlines(keepends=True)
    for index, line in enumerate(lines):
        match = re.match(r'^(\s*)"([^"\n]+)"\s*:\s*(\{.*\})(,?)(\r?\n)?$', line)
        if not match or match[2] not in changes:
            continue
        key = match[2]
        if key in seen:
            raise RuntimeError(f"Duplicate target definition: {key}")
        before = json.loads(match[3])
        if before != changes[key]:
            separators = (",", ":") if compact else (", ", ": ")
            value = json.dumps(changes[key], ensure_ascii=False, separators=separators)
            lines[index] = f'{match[1]}"{key}": {value}{match[4]}{match[5] or ""}'
        seen.add(key)
    if seen != changes.keys():
        raise RuntimeError(f"Missing target definitions: {changes.keys() - seen}")
    path.write_bytes("".join(lines).encode("utf-8"))


def pattern(value):
    return {"type": "string", "pattern": value}


def authentication(value, instance=False):
    p = value["properties"]
    p["sourceType"] = {"const": "network", "description": "由插件握手声明；宿主按此选择网络音频读取。"}
    if "sourceType" not in value["required"]:
        value["required"].append("sourceType")
    p["platform"]["enum"] = ["netease", "qqmusic", "applemusic", "bilibili"]
    p["authentication"]["enum"] = ["platformAccountRequired", "anonymous"]
    p["capabilities"].pop("minItems", None)
    anonymous = {"authentication": {"const": "anonymous"}}
    if instance:
        p["pluginId"]["enum"] = ["musicfs.plugin.source." + name for name in p["platform"]["enum"]]
        anonymous["accountId"] = {"const": ""}
    value["allOf"] = [{"if": {"properties": {"platform": {"const": "bilibili"}}}, "then": {"properties": anonymous}, "else": {"properties": {"authentication": {"const": "platformAccountRequired"}}}}]


def metadata_definitions(defs, prefix=""):
    touched = {}
    for name in ["Track", "Playlist", "Entry", "TrackLookup", "PlaylistTrack", "Observation", "SearchPage", "PlaylistTracksPage"]:
        key = prefix + name
        item = defs[key]
        p = item["properties"]
        if name == "Track":
            p["externalId"], p["sourceUrl"] = pattern(TRACK), pattern(TRACK_URL)
            item["description"] = "Bilibili 曲目使用 video:<BV>:<cid> 或 audio:<sid>；sourceUrl 是固定官方资料链接，不是播放地址。具体 ID 与链接对应关系还由 SDK 校验。"
        elif name == "Playlist":
            p["externalId"], p["sourceUrl"] = pattern(PLAYLIST), pattern(PLAYLIST_URL)
            item["required"] = [field for field in item["required"] if field != "trackCount"]
            p["trackCount"]["description"] = "真实总条目数；不完整观察无法确认总数时为null或省略，不得猜测剩余数量。"
            item["description"] = "Bilibili 收藏夹为 favorite:<media_id>，音频歌单为 audio-menu:<menu_id>；相同数字在不同命名空间中不相同。"
        elif name == "Entry":
            p["externalTrackId"] = pattern(ENTRY)
            item["description"] = "保留顺序、分 P 及重复出现项；unavailable:<type>:<aid> 仅是不可用收藏资源占位，不能用于播放。"
        elif name in ["TrackLookup", "PlaylistTrack"]:
            p["externalId"] = pattern(ENTRY)
            item["allOf"] = [{"if": {"properties": {"externalId": {"pattern": "^unavailable:"}}}, "then": {"properties": {"track": {"type": "null"}, "errorCode": {"const": "NOT_FOUND"}}}}]
        elif name == "Observation":
            item["allOf"] = [{"if": {"properties": {"complete": {"const": True}}}, "then": {"properties": {"playlist": {"required": ["trackCount"], "properties": {"trackCount": {"type": "integer", "minimum": 0}}}}}}]
        else:
            p["nextCursor"]["anyOf"][1]["pattern"] = CURSOR
        touched[key] = item
    return touched


def update_http():
    relative = "docs/contracts/http.schema.json"
    defs = load(relative)["$defs"]
    changes = metadata_definitions(defs, "Online")
    for name in ["OnlineDescriptor", "OnlineInstance"]:
        authentication(defs[name], name == "OnlineInstance")
        changes[name] = defs[name]
    defs["OnlineTrackLookupRequest"]["properties"]["externalIds"]["items"] = pattern(ENTRY)
    changes["OnlineTrackLookupRequest"] = defs["OnlineTrackLookupRequest"]
    for name, external in [("ImportRemotePlaylistRequest", PLAYLIST), ("RemotePlaylist", PLAYLIST), ("RemoteSource", ENTRY)]:
        p = defs[name]["properties"]
        p["externalId"] = pattern(external)
        p["accountId"].pop("minLength", None)
        p["accountId"]["description"] = "Bilibili 匿名来源固定为空；需要平台账号的来源仍由宿主校验当前账号。"
        if name == "RemotePlaylist":
            p["platform"] = {"enum": ["netease", "bilibili"]}
        if name == "ImportRemotePlaylistRequest":
            defs[name]["allOf"] = [{"if": {"properties": {"externalId": {"pattern": "^(?:favorite|audio-menu):"}}}, "then": {"properties": {"accountId": {"const": ""}}}, "else": {"properties": {"accountId": {"minLength": 1}}}}]
        changes[name] = defs[name]
    defs["RecommendationSource"]["properties"]["platform"]["enum"] = ["netease", "qqmusic", "applemusic", "bilibili"]
    changes["RecommendationSource"] = defs["RecommendationSource"]
    codes = defs["RecommendationFailure"]["properties"]["code"]["enum"]
    if "RISK_CONTROL" not in codes:
        codes.append("RISK_CONTROL")
    changes["RecommendationFailure"] = defs["RecommendationFailure"]
    defs["ErrorDetail"]["properties"]["code"]["description"] = "RISK_CONTROL 表示上游风控拒绝访问，HTTP 403；不能转换为空结果或绕过限制。"
    changes["ErrorDetail"] = defs["ErrorDetail"]
    replace_lines(relative, changes)


def update_plugin():
    relative = "docs/contracts/plugins/v1/online.schema.json"
    defs = load(relative)["$defs"]
    changes = metadata_definitions(defs)
    authentication(defs["Descriptor"])
    changes["Descriptor"] = defs["Descriptor"]
    defs["Reference"]["properties"]["externalId"] = pattern(REFERENCE)
    defs["TrackBatchRequest"]["properties"]["externalIds"]["items"] = pattern(ENTRY)
    defs["PlaylistTracksRequest"]["properties"]["externalId"] = pattern(PLAYLIST)
    for name in ["Reference", "TrackBatchRequest", "PlaylistTracksRequest"]:
        changes[name] = defs[name]
    m = defs["Manifest"]
    m["properties"]["sourceType"] = {"const": "network"}
    if "sourceType" not in m["required"]:
        m["required"].append("sourceType")
    m["properties"]["pluginId"]["enum"] = ["musicfs.plugin.source." + name for name in ["netease", "qqmusic", "applemusic", "bilibili"]]
    old_caps = [name for name in m["properties"]["capabilities"]["items"]["enum"] if name != "stream.resolve"]
    m["properties"]["capabilities"]["items"]["enum"] = old_caps + ["stream.resolve"]
    m["properties"]["capabilities"]["maxItems"] = 17
    m["allOf"] = [
        {"if": {"properties": {"pluginId": {"const": "musicfs.plugin.source.netease"}}}, "then": {"properties": {"capabilities": {"items": {"enum": old_caps}}}}},
        {"if": {"properties": {"pluginId": {"const": "musicfs.plugin.source.bilibili"}}}, "then": {"properties": {"capabilities": {"items": {"enum": BILI_CAPS}}}}},
        {"if": {"properties": {"pluginId": {"enum": ["musicfs.plugin.source.qqmusic", "musicfs.plugin.source.applemusic"]}}}, "then": {"properties": {"capabilities": {"const": []}}}},
    ]
    changes["Manifest"] = m
    codes = defs["Failure"]["properties"]["error"]["properties"]["data"]["properties"]["code"]["enum"]
    if "RISK_CONTROL" not in codes:
        codes.append("RISK_CONTROL")
    changes["Failure"] = defs["Failure"]
    replace_lines(relative, changes, compact=True)


def update_audio_wire():
    relative = "docs/contracts/plugins/v1/online.schema.json"
    defs = load(relative)["$defs"]
    def obj(properties):
        return {"type": "object", "additionalProperties": False, "required": list(properties), "properties": properties}
    def ref(name):
        return {"$ref": "#/$defs/" + name}
    nullable_duration = {"anyOf": [{"type": "null"}, {"type": "integer", "minimum": 1, "maximum": 86400000}]}
    audio = obj({"externalId": pattern(TRACK), "mime": {"type": "string"}, "codec": {"type": "string"}, "container": {"type": "string"}, "sizeBytes": {"type": "integer", "minimum": -1, "maximum": 268435456}, "durationMs": nullable_duration, "bitrateBps": {"anyOf": [{"type": "null"}, {"type": "integer", "minimum": 1, "maximum": 100000000}]}})
    audio["oneOf"] = [{"properties": {"mime": {"const": mime}, "codec": {"const": codec}, "container": {"const": container}}} for mime, codec, container in [("audio/mp4", "aac", "m4a"), ("audio/mpeg", "mp3", "mp3"), ("audio/flac", "flac", "flac"), ("audio/wav", "pcm_s16le", "wav")]]
    audio["description"] = "完整音轨描述；-1或0长度表示未知，不包含上游URL或凭据。公开HTTP资产仍保持未验证的字节身份。"
    stream_id = pattern(r"^[0-9a-f]{48}$")
    added = {
        "AudioDescriptor": audio,
        "AudioOpened": obj({"audio": ref("AudioDescriptor"), "streamId": stream_id}),
        "AudioRead": obj({"context": ref("Context"), "streamId": stream_id, "offset": {"type": "integer", "minimum": 0, "maximum": 268435456}}),
        "AudioChunk": obj({"offset": {"type": "integer", "minimum": 0, "maximum": 268435456}, "data": {"type": ["string", "null"], "contentEncoding": "base64", "pattern": r"^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$", "maxLength": 87384, "if": {"minLength": 87384}, "then": {"pattern": "==$"}, "description": "每块解码后至多65536字节；关闭响应允许null。"}, "eof": {"type": "boolean"}}),
        "AudioOpenedResult": obj({"context": ref("Context"), "value": ref("AudioOpened")}),
        "AudioDescriptorResult": obj({"context": ref("Context"), "value": ref("AudioDescriptor")}),
        "AudioChunkResult": obj({"context": ref("Context"), "value": ref("AudioChunk")}),
    }
    existing = {key: value for key, value in added.items() if key in defs}
    if existing:
        replace_lines(relative, existing, compact=True)
    missing = {key: value for key, value in added.items() if key not in defs}
    if missing:
        path = ROOT / relative
        raw = path.read_bytes().decode("utf-8")
        newline = "\r\n" if "\r\n" in raw else "\n"
        marker = '  "$defs": {' + newline
        if raw.count(marker) != 1:
            raise RuntimeError("Online definitions insertion point is ambiguous")
        content = "".join(f'    "{key}": {json.dumps(value, ensure_ascii=False, separators=(",", ":"))},{newline}' for key, value in missing.items())
        path.write_bytes(raw.replace(marker, marker + content).encode("utf-8"))
    for method, params in [("audioDescribe", "Reference"), ("audioOpen", "Reference"), ("audioRead", "AudioRead"), ("audioClose", "AudioRead")]:
        method = "provider.online." + method
        if not any(item["properties"]["method"].get("const") == method for item in defs["Request"]["oneOf"]):
            defs["Request"]["oneOf"].append(obj({"jsonrpc": {"const": "2.0"}, "id": {"type": "string", "minLength": 1, "maxLength": 80}, "method": {"const": method}, "params": ref(params)}))
    results = defs["Success"]["properties"]["result"]["oneOf"]
    for name in ["AudioDescriptorResult", "AudioOpenedResult", "AudioChunkResult"]:
        if ref(name) not in results:
            results.append(ref(name))
    replace_lines(relative, {"Request": defs["Request"], "Success": defs["Success"]}, compact=True)


def update_openapi():
    relative = "docs/contracts/openapi.yaml"
    doc = load(relative)
    paths = doc["paths"]
    base = "/online-instances/{id}"
    descriptions = {
        "/online-instances": "只返回本人在线实例。sourceType=network 由插件握手声明，播放按此选择网络读取。Bilibili 为 anonymous，账号字段为空，不要求平台登录；其他平台沿用已校验账号约束。available 表示实例可用，每项能力以插件声明为准，不代表任意内容可访问。程序路径、配置摘要与账号凭据不公开。",
        "/provider-instances": "返回插件提供的 sourceType、导入配置声明、已合成的实例默认值与授权根编号/显示名。已握手 sourceType 为 local 或 network；从未完成握手时省略，不能据此播放。宿主按插件握手声明选择读取方式，不依据平台名称；部署配置不能覆盖。无路径、凭据或真实根身份。",
        base + "/search": "按实例查询资料，保留上游顺序与有界分页。Bilibili 仅支持准确公开 BV/AV/au/ml/am 编号及受支持官方链接，支持曲目和收藏夹/音频歌单查询，不提供关键词搜索。结果曲目规范为 video:<BV>:<cid> 或 audio:<sid>；集合规范为 favorite:<media_id> 或 audio-menu:<menu_id>。Bilibili 匿名查询，其他平台要求已校验账号；风控返回 RISK_CONTROL（403），不得变成成功空页。游标绑定实例与查询，过期返回 CURSOR_EXPIRED。",
        base + "/tracks/{externalId}": "读取曲目元数据；Bilibili 接受规范 video:<BV>:<cid> 或 audio:<sid>，不要求平台账号。metadataDigest 不是音频资产版本，playbackAvailability 固定 unknown，sourceUrl 仅指官方资料页；读取元数据不读取音频正文。上游访问限制按结构化错误返回。",
        base + "/playlists/{externalId}": "读取一次 unversioned 观察，保留视频分 P、重复位置及 unavailable:<type>:<aid> 失效占位，最多10000项。Bilibili 接受 favorite:<media_id> 或 audio-menu:<menu_id>，不要求平台账号；占位不能作为可播放曲目。complete 不保证远端固定快照，也不自动导入曲库或删除本地条目。",
        base + "/playlists/{externalId}/tracks": "分页读取集合的完整观察，保留分 P、顺序、重复项与 NOT_FOUND 失效占位。Bilibili 使用匿名收藏夹/音频歌单命名空间；游标绑定实例、集合和五分钟内的内存快照。complete 描述整次观察，不能把当前页当作全部；读取不导入曲库、不读取音频正文。",
        base + "/tracks/lookup": "只读 POST，按会话及 CSRF 校验。接受1..50个规范曲目或失效占位身份，保留顺序及重复；缺失和 unavailable:<type>:<aid> 逐项返回 NOT_FOUND。Bilibili 匿名来源无需平台账号；不接受任意网络地址，不返回音频 URL。",
        base + "/tracks/{externalId}/details": "读取当前用户可见来源的只读插件详情。Bilibili 为匿名公开内容，其他平台检查账号及请求前后登录代次；字段仅用于纯文本展示，不授予音频资源。",
    }
    changes = {}
    for path, description in descriptions.items():
        item = paths[path]
        method = "post" if path.endswith("/lookup") else "get"
        item[method]["description"] = description
        if "200" in item[method].get("responses", {}):
            item[method]["responses"]["200"]["description"] = "当前用户可见的来源资料"
        changes[path] = item
    for path in [base + "/tracks/{externalId}", base + "/tracks/{externalId}/details", base + "/tracks/{externalId}/resolve", base + "/playlists/{externalId}", base + "/playlists/{externalId}/tracks"]:
        item = paths[path]
        params = item.get("parameters", item.get("get", {}).get("parameters", []))
        for parameter in params:
            if parameter.get("name") == "externalId":
                parameter["schema"] = pattern(PLAYLIST if "/playlists/" in path else TRACK)
        changes[path] = item
    imported = paths["/remote-playlists"]
    imported["post"]["description"] = "记录明确导入意图，不访问远端音频。Bilibili 使用空 accountId 与规范 favorite:<media_id> / audio-menu:<menu_id>，其他平台固定当前已校验账号；不能借匿名来源指定其他账号。相同数字的两类 Bilibili 集合保持独立身份。"
    changes["/remote-playlists"] = imported
    resolve = paths[base + "/tracks/{externalId}/resolve"]
    resolve["post"]["description"] = "按来源能力将公开完整音轨建立为本地 SourceTrack 描述，成功返回200；未支持或受限制内容返回结构化错误，不使用试看或视频混合流替代。Bilibili 当前仅交付公开独立音频。元数据查询、解析及创建播放会话不读取音频正文；GET stream 才按需代理读取。在线表示为 proxy/none，采样率、声道、长度未知时保留未知，不承诺 ETag、Range 或跨请求字节一致性。请求体为{}，校验会话、CSRF和Origin。"
    resolve["post"]["responses"]["200"] = {"description": "可用于播放协商的来源曲目", "content": {"application/json": {"schema": {"$ref": "./http.schema.json#/$defs/SourceTrack"}}}}
    changes[base + "/tracks/{externalId}/resolve"] = resolve
    error = doc["components"]["responses"]["Error"]
    error["description"] = "结构化业务错误。400 INVALID_ARGUMENT，401 AUTH_REQUIRED，403 ACCESS_DENIED/PLATFORM_AUTH_REQUIRED/RISK_CONTROL，404 NOT_FOUND，409 CONFLICT/STALE_ATTEMPT/RESOURCE_CHANGED，410 CURSOR_EXPIRED/SESSION_EXPIRED，415 UNSUPPORTED，429 RATE_LIMITED，503 TEMPORARY_FAILURE/SOURCE_OFFLINE。RISK_CONTROL 为上游风控拒绝，不自动重试或绕过限制。"
    changes["Error"] = error
    replace_lines(relative, changes)


def update_source_type_fixtures():
    for relative in ["tests/contract/plugins/online_test.go"]:
        path = ROOT / relative
        raw = path.read_bytes()
        raw = raw.replace(b'ProtocolVersion: "1.0", Roles: []string{"provider"}', b'ProtocolVersion: "1.0", SourceType: pluginproto.SourceNetwork, Roles: []string{"provider"}')
        path.write_bytes(raw)
    path = ROOT / "tests/testdata/contracts/plugins/samples.json"
    raw = path.read_bytes()
    raw = raw.replace(b'"roles":["provider"],"capabilities"', b'"roles":["provider"],"sourceType":"local","capabilities"')
    path.write_bytes(raw)
    path = ROOT / "tests/testdata/contracts/api/samples.json"
    raw = path.read_bytes()
    raw = re.sub(rb'(?:"sourceType":"local",)+', b'"sourceType":"local",', raw)
    if b'"sourceType"' not in raw:
        raw = raw.replace(b'"capabilities":["library.scan","metadata.read","stream.resolve"],"availability"', b'"sourceType":"local","capabilities":["library.scan","metadata.read","stream.resolve"],"availability"')
    path.write_bytes(raw)


if __name__ == "__main__":
    update_http()
    update_plugin()
    update_audio_wire()
    update_openapi()
    update_source_type_fixtures()
    print("Bilibili metadata contracts updated")
