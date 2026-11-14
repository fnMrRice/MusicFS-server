"""维护曲目、歌手、专辑及来源的 HTTP 契约。"""

import runpy
from pathlib import Path


helpers = runpy.run_path(str(Path(__file__).with_name("update-works-contracts.py")))
ref, obj, text = helpers["ref"], helpers["obj"], helpers["text"]
sync, root = helpers["sync_entries"], helpers["ROOT"]


def nullable(schema):
    return {"anyOf": [schema, {"type": "null"}]}


def array(schema, **options):
    return {"type": "array", "items": schema, **options}


def input_object(fields, required):
    return {**obj(fields), "required": required}


id_schema = text(128, 1)
positive = {"type": "integer", "minimum": 1}
count = {"type": "integer", "minimum": 0}
created_updated = {"createdAt": ref("Timestamp"), "updatedAt": ref("Timestamp")}
track_fields = {
    "title": text(256, 1), "coverPath": nullable(text(4096, 1)),
    "genre": nullable(text(128, 1)),
    "year": nullable({"type": "integer", "minimum": 1, "maximum": 9999}),
    "rating": nullable({"type": "integer", "minimum": 1, "maximum": 5}),
}
track_input = {
    **track_fields, "artistNames": array(text(256, 1), uniqueItems=True, maxItems=128),
    "albumIds": array(id_schema, uniqueItems=True, maxItems=128),
}
album_fields = {
    "name": text(256, 1), "externalId": nullable(text(256, 1)), "isExternal": {"type": "boolean"},
    "coverPath": nullable(text(4096, 1)), "publisher": nullable(text(256, 1)),
    "releaseDate": nullable({"type": "string", "format": "date"}),
}
artist = {"artistId": id_schema, "name": text(256, 1)}
album = {"albumId": id_schema, **album_fields, "revision": positive}
source = {
    "sourceTrackId": id_schema, "pluginId": nullable(text(128, 1)),
    "providerInstanceId": ref("InstanceID"), "sourceId": {"type": "string", "minLength": 1},
    **created_updated,
}
definitions = {
    "MusicArtist": obj(artist),
    "MusicArtistEntry": obj({**artist, "artistId": text(128), "name": text(256), "trackCount": count}),
    "MusicAlbum": obj(album),
    "MusicAlbumEntry": obj({**album, "albumId": text(128), "name": text(256), "revision": count, "trackCount": count}),
    "MusicGenreEntry": obj({"name": {"type": "string"}, "trackCount": count}),
    "MusicSource": obj(source),
    "MusicTrack": obj({
        "trackId": id_schema, **track_fields, **created_updated, "revision": positive,
        "artists": array(ref("MusicArtist")), "albums": array(ref("MusicAlbum")),
        "sources": array(ref("MusicSource")), "selectedSource": nullable(ref("SourceTrack")),
    }),
    "CreateMusicTrack": input_object(track_input, ["title"]),
    "UpdateMusicTrack": input_object({**track_input, "expectedRevision": positive}, ["title", "expectedRevision"]),
    "CreateMusicAlbum": input_object(album_fields, ["name"]),
    "UpdateMusicAlbum": input_object({**album_fields, "expectedRevision": positive}, ["name", "expectedRevision"]),
}
for kind, item in (("Track", "MusicTrack"), ("Artist", "MusicArtistEntry"), ("Album", "MusicAlbumEntry"), ("Genre", "MusicGenreEntry")):
    page = {"items": array(ref(item), maxItems=200), "nextAfter": nullable(text(1024 if kind == "Track" else 128, 1))}
    if kind == "Track":
        page.update({"libraryRevision": {"type": "string"}, "matchingRevision": count})
    definitions[f"Music{kind}Page"] = obj(page)


def operation(operation_id, schema, description, request=None, status="200"):
    op = helpers["operation"](operation_id, schema, description, request)
    if status == "201":
        op["responses"]["201"] = op["responses"].pop("200")
    return op


paths = {"/library/sources/{id}/track": {
    "parameters": [{"$ref": "#/components/parameters/ID"}],
    "get": operation("getMusicTrackForSource", "MusicTrack", "读取来源当前所属的曲目。"),
}}
for kind, route in (("Track", "tracks"), ("Artist", "artists"), ("Album", "albums"), ("Genre", "genres")):
    path = "/library/" + route
    model = "Music" + kind
    list_op = operation("list" + model + "s", model + "Page", "按账号和普通音乐的展示范围分页。")
    params = {"after": text(1024 if kind == "Track" else 128, 1),
              "limit": {"type": "integer", "minimum": 1, "maximum": 200, "default": 50},
              "q": text(256, 1), "providerInstanceId": {"$ref": "./http.schema.json#/$defs/InstanceID"}}
    if kind == "Track":
        params.update({"artistId": id_schema, "albumId": id_schema, "genre": text(128, 1),
                       "unknown": {"enum": ["artist", "album", "genre"]}})
        list_op["description"] = "按曲名及稳定 ID 分页。无来源的曲目也可浏览。选源与曲目元数据独立；游标绑定账号、筛选条件和修订。"
    list_op["parameters"] = [{"in": "query", "name": k, "schema": v} for k, v in params.items()]
    paths[path] = {"get": list_op}
    if kind in ("Track", "Album"):
        paths[path]["post"] = operation("create" + model, model, "创建本人的资料记录。", "Create" + model, "201")
        paths[path + "/{id}"] = {
            "parameters": [{"$ref": "#/components/parameters/ID"}],
            "get": operation("get" + model, model, "按内部编号读取本人资料。"),
            "put": operation("update" + model, model, "按预期修订保存资料，保留来源和播放偏好。", "Update" + model),
        }
    elif kind == "Artist":
        paths[path + "/{id}"] = {"parameters": [{"$ref": "#/components/parameters/ID"}],
                                 "get": operation("getMusicArtist", model, "按内部编号读取本人曲库中的歌手。")}

sync(root / "docs/contracts/http.schema.json", "$defs", definitions)
sync(root / "docs/contracts/openapi.yaml", "paths", paths)
print("Music library contracts updated")
