"""组装和暂存已审阅的日志快照，不改写共享工作树。"""

import hashlib
import io
import json
import shutil
import subprocess
import sys
import tarfile
from pathlib import Path

root = Path(__file__).resolve().parents[1]
folder = root / "build/logging-commit"


def git(*args, data=None):
    return subprocess.check_output(["git", *args], cwd=root, input=data)


def verify(manifest):
    assert git("rev-parse", "HEAD").decode().strip() == manifest["head"], "HEAD changed"
    for name, item in manifest["files"].items():
        assert not Path(name).is_absolute() and ".." not in Path(name).parts
        assert "\n" not in name and "\t" not in name
        assert hashlib.sha256((root / name).read_bytes()).hexdigest() == item["source_sha256"], f"Working file changed: {name}"
        assert hashlib.sha256(Path(item["snapshot"]).read_bytes()).hexdigest() == item["snapshot_sha256"], f"Snapshot changed: {name}"


if len(sys.argv) > 1 and sys.argv[1] == "stage-parent":
    parent = root.parent
    manifest = json.loads((folder / "root/parent-manifest.json").read_text(encoding="utf-8"))

    def parent_git(*args, data=None):
        return subprocess.check_output(["git", *args], cwd=parent, input=data)

    assert parent_git("rev-parse", "HEAD").decode().strip() == manifest["head"], "Parent HEAD changed"
    assert not parent_git("diff", "--cached", "--name-only").strip(), "Parent index has unrelated changes"
    assert hashlib.sha256((parent / manifest["path"]).read_bytes()).hexdigest() == manifest["source_sha256"], "Parent architecture changed"
    child = git("rev-parse", "HEAD").decode().strip()
    assert len(sys.argv) == 3 and child == sys.argv[2], "Unexpected server commit"
    blob = parent_git("hash-object", "-w", "--stdin", "--path=" + manifest["path"], data=Path(manifest["snapshot"]).read_bytes()).decode().strip()
    entries = f"100644 {blob}\t{manifest['path']}\n160000 {child}\tserver\n"
    parent_git("update-index", "--index-info", data=entries.encode())
    print("Staged parent logging documentation and server revision")
    sys.exit(0)


if len(sys.argv) > 1 and sys.argv[1] == "stage":
    manifest = json.loads((folder / "combined.json").read_text(encoding="utf-8"))
    verify(manifest)
    assert not git("diff", "--cached", "--name-only").strip(), "Index has unrelated changes"
    entries = []
    for name, item in manifest["files"].items():
        blob = git("hash-object", "-w", "--stdin", "--path=" + name, data=Path(item["snapshot"]).read_bytes()).decode().strip()
        entries.append(f"100644 {blob}\t{name}\n")
    git("update-index", "--index-info", data="".join(entries).encode())
    print(f"Staged {len(entries)} logging files")
    sys.exit(0)

manifest = {"head": git("rev-parse", "HEAD").decode().strip(), "files": {}}
for area in ("root", "plugin", "config", "business"):
    source_path = folder / area / ("snapshot-map.json" if area == "business" else "manifest.json")
    source = json.loads(source_path.read_text(encoding="utf-8"))
    assert source["head"] == manifest["head"], f"Outdated snapshot: {area}"
    for name, original in source["files"].items():
        item = dict(original)
        item["snapshot_sha256"] = hashlib.sha256(Path(item["snapshot"]).read_bytes()).hexdigest()
        if name in manifest["files"]:
            existing = Path(manifest["files"][name]["snapshot"]).read_text(encoding="utf-8")
            assert existing == Path(item["snapshot"]).read_text(encoding="utf-8"), f"Conflicting snapshot: {name}"
        manifest["files"][name] = item
verify(manifest)
destination = folder / "check"
assert not destination.exists(), "Check directory already exists"
destination.mkdir()
with tarfile.open(fileobj=io.BytesIO(git("archive", "--format=tar", manifest["head"]))) as archive:
    archive.extractall(destination, filter="data")
for name, item in manifest["files"].items():
    output = destination / name
    output.parent.mkdir(parents=True, exist_ok=True)
    shutil.copyfile(item["snapshot"], output)
(folder / "combined.json").write_text(json.dumps(manifest, ensure_ascii=False, indent=2), encoding="utf-8")
print(f"Prepared isolated logging tree with {len(manifest['files'])} changed files: {destination}")
