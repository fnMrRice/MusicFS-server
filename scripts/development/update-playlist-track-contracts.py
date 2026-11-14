"""维护普通歌单的曲目引用和可选的强制来源。"""

import json
import runpy
from pathlib import Path

helpers = runpy.run_path(str(Path(__file__).with_name("update-works-contracts.py")))
root = helpers["ROOT"]
schema = root / "docs/contracts/http.schema.json"
definitions = json.loads(schema.read_text(encoding="utf-8"))["$defs"]
identifier = helpers["text"](128, 1)
empty = {"type": "null"}
updates = {}
for suffix in ("Ref", "Input"):
    prior = definitions["PlaylistSource" + suffix]
    track = {
        "type": "object", "additionalProperties": False,
        "properties": {
            "trackId": identifier, "forcedSourceTrackId": {"anyOf": [identifier, empty]},
            "sourceTrackId": empty, "remoteSourceId": empty,
        },
        "required": ["trackId", *prior["required"]],
    }
    source = {**prior, "additionalProperties": False}
    updates["PlaylistEntry" + suffix] = {"oneOf": [track, source]}
for model, suffix in (
    ("PlaylistLocalEntry", "Ref"), ("PlaylistLocalEntryInput", "Input"),
    ("PlaylistProvenance", "Ref"), ("PlaylistAggregateEntry", "Ref"),
    ("PlaylistHiddenRule", "Ref"), ("HidePlaylistRequest", "Input"),
):
    definition = definitions[model]
    definition["properties"]["source"] = helpers["ref"]("PlaylistEntry" + suffix)
    updates[model] = definition
helpers["sync_entries"](schema, "$defs", updates)
print("Playlist entry contracts updated")
