"""Update narrow metadata contracts and fold the oldest standalone migration."""
from pathlib import Path
import json
import subprocess

root = Path(__file__).resolve().parents[1]
schema = root / "docs/contracts/http.schema.json"
text = schema.read_text(encoding="utf-8")
lines = text.splitlines(keepends=True)
for index, line in enumerate(lines):
    if line.lstrip().startswith('"FingerprintIdentity":'):
        value = json.loads("{" + line.strip().rstrip(",") + "}")["FingerprintIdentity"]
        value["required"] = ["analyzer" if item == "fpcalc" else item for item in value["required"]]
        props = value["properties"]
        if "fpcalc" in props:
            props["analyzer"] = props.pop("fpcalc")
        for name in ["algorithm", "algorithmVersion", "algorithmConfig"]:
            props[name] = {"type": "string", "minLength": 1, "maxLength": 128, "pattern": "^[^\x00\r\n]+$"}
        lines[index] = '    "FingerprintIdentity": ' + json.dumps(value, ensure_ascii=False) + ',\n'
    if line.lstrip().startswith('"MetadataCandidate":'):
        value = json.loads("{" + line.strip().rstrip(",") + "}")["MetadataCandidate"]
        value["properties"]["origin"] = {"type": "string", "minLength": 1, "maxLength": 64}
        value["allOf"] = [
            {"if": {"properties": {"origin": {"const": "container"}}}, "then": {"properties": {"field": {"const": "artwork"}, "basis": {"const": "containerEmbedded"}}}},
            {"if": {"properties": {"basis": {"const": "containerEmbedded"}}}, "then": {"properties": {"origin": {"const": "container"}}}},
        ]
        lines[index] = '    "MetadataCandidate": ' + json.dumps(value, ensure_ascii=False) + ',\n'
    if line.lstrip().startswith('"ScraperInstance":'):
        value = json.loads("{" + line.strip().rstrip(",") + "}")["ScraperInstance"]
        value["properties"]["pluginId"] = {"type": "string", "minLength": 1, "maxLength": 128}
        value["properties"]["queryCorrectionsSupported"] = {"type": "boolean"}
        lines[index] = '    "ScraperInstance": ' + json.dumps(value, ensure_ascii=False) + ',\n'
schema.write_text("".join(lines), encoding="utf-8", newline="\n")

protocol = root / "docs/contracts/plugins/v1/protocol.schema.json"
lines = protocol.read_text(encoding="utf-8").splitlines(keepends=True)
for index, line in enumerate(lines):
    if line.lstrip().startswith('"ScrapeCandidate":'):
        value = json.loads("{" + line.strip().rstrip(",") + "}")["ScrapeCandidate"]
        value["properties"]["origin"] = {"type": "string", "minLength": 1, "maxLength": 64}
        lines[index] = '    "ScrapeCandidate": ' + json.dumps(value, ensure_ascii=False) + ',\n'
protocol.write_text("".join(lines), encoding="utf-8", newline="\n")

migrations = root / "plugins/database/sqlite/migrations"
old = migrations / "029_batch_011_029.sql"
new = migrations / "030_batch_011_030.sql"
step = migrations / "030_recording_default_sources.sql"
if old.exists() and step.exists():
    subprocess.run(["git", "mv", str(old.relative_to(root)), str(new.relative_to(root))], cwd=root, check=True)
    with new.open("a", encoding="utf-8", newline="\n") as out:
        out.write("\n-- migration: 030_recording_default_sources.sql\n")
        out.write(step.read_text(encoding="utf-8"))
    # Folding SQL into the historical batch is a merge, not a file move.
    step.unlink()
