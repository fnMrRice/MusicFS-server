"""从固定 HEAD 提取业务日志快照，不修改工作树源码或 Git 暂存区。"""

import difflib
import hashlib
import json
import re
import subprocess
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
OUTPUT = ROOT / "build/logging-commit/business"
EXPECTED_HEAD = "f7e9e97a241ca4ed79e154840eab25f9d2a5873d"

DIRECT_STAGE = [
    "src/library/jobs/logging.go",
    "src/library/jobs/service.go",
    "src/library/playlists/service.go",
    "src/library/playlists/worker.go",
    "src/library/scan.go",
    "src/metadata/enrichmentworker/automatic.go",
    "src/metadata/enrichmentworker/execute.go",
    "src/metadata/enrichmentworker/service.go",
    "src/metadata/identification/fingerprintworker/compute.go",
    "src/metadata/identification/fingerprintworker/service.go",
    "src/metadata/identification/hashworker/compute.go",
    "src/metadata/identification/hashworker/service.go",
    "src/metadata/writeback/service.go",
    "src/metadata/writebackhost/recovery.go",
    "src/metadata/writebackhost/service.go",
    "src/playback/control.go",
    "src/playback/logging.go",
    "src/playback/service.go",
    "src/storage/mediastore/collector.go",
    "src/storage/mediastore/content_service.go",
    "src/storage/mediastore/publication.go",
    "tests/integration/bootstrap/managed_logging_test.go",
    "tests/integration/library/scan_failure_test.go",
    "tests/integration/plugins/scan_coordination_test.go",
    "tests/unit/jobs/logging_test.go",
]

EXCLUDED = [
    "src/playback/models.go",
    "src/playback/timeline.go",
    "src/playback/timeline/validation.go",
    "src/playback/processedcache/lease.go",
    "src/playback/processedcache/manager.go",
    "src/playback/processedcache/models.go",
    "src/playback/processedcache/restore.go",
    "src/playback/processedcache/writer.go",
]


def git(*args):
    return subprocess.check_output(["git", *args], cwd=ROOT)


def base(path):
    return git("show", EXPECTED_HEAD + ":" + path).decode("utf-8")


def current(path):
    return (ROOT / path).read_text(encoding="utf-8")


def replace_once(text, old, new):
    if text.count(old) != 1:
        raise RuntimeError("Snapshot anchor changed: " + repr(old))
    return text.replace(old, new, 1)


def function(text, prefix):
    start = text.index(prefix)
    if start != 0 and text[start - 1] != "\n":
        raise RuntimeError("Function anchor is not at the start of a line")
    following = re.search(r"\n(?:func|type|var|const) ", text[start + len(prefix):])
    end = len(text) if following is None else start + len(prefix) + following.start() + 1
    return text[start:end]


def replace_function(text, working, prefix):
    return replace_once(text, function(text, prefix), function(working, prefix))


def add_slog(text):
    return replace_once(text, '\t"context"\n', '\t"context"\n\t"log/slog"\n')


def sha256(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


head = git("rev-parse", "HEAD").decode().strip()
if head != EXPECTED_HEAD:
    raise RuntimeError("HEAD changed; re-review the snapshot selection")

# 这些函数的当前差异仅为日志；声道、输出身份等相邻函数继续使用 HEAD。
selected = {
    "src/playback/cache.go": ["func (s *Service) processCached("],
    "src/playback/create.go": ["func (s *Service) Create("],
    "src/playback/stream.go": ["func (s *Stream) Process(", "func (s *Service) OpenStream("],
}
snapshots = {}
for path, prefixes in selected.items():
    text = add_slog(base(path))
    for prefix in prefixes:
        text = replace_function(text, current(path), prefix)
    snapshots[path] = text

# prepare 和 recovery 的同一个函数同时存在声道改动，逐段提取日志。
path = "src/playback/prepare.go"
text = add_slog(base(path))
text = replace_once(text, "\tdefer closeInput()\n", "\tdefer closeInput()\n" +
    '\tslog.DebugContext(ctx, "Playback input prepared", "sessionId", task, "instanceId", t.ProviderID, "sizeBytes", size, "decoded", decrypted)\n')
text = replace_once(text, "\tok = true\n\treturn p, nil\n", "\tok = true\n" +
    '\tif slog.Default().Enabled(ctx, slog.Level(-8)) {\n' +
    '\t\tslog.Log(ctx, slog.Level(-8), "Playback representation prepared", "sessionId", task, "sizeBytes", n, "processed", processed)\n' +
    '\t}\n\treturn p, nil\n')
snapshots[path] = text

path = "src/playback/recovery.go"
text = add_slog(base(path))
text = replace_once(text, "\t\tprepared = nil\n\t}\n\treturn result, nil\n",
    "\t\tprepared = nil\n\t}\n" +
    '\tslog.InfoContext(ctx, "Playback source recovery decided", "sessionId", id, "decision", result.Decision, "reason", result.Reason)\n' +
    "\treturn result, nil\n")
snapshots[path] = text

working_hashes = {path: sha256(ROOT / path) for path in [*DIRECT_STAGE, *snapshots, *EXCLUDED]}
mapping = {}
for path, text in snapshots.items():
    target = OUTPUT / path
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_text(text, encoding="utf-8", newline="\n")
    mapping[path] = str(target)
subprocess.run(["gofmt", "-w", *mapping.values()], cwd=ROOT, check=True)

patch = []
for path, target in mapping.items():
    patch.extend(difflib.unified_diff(base(path).splitlines(keepends=True),
        Path(target).read_text(encoding="utf-8").splitlines(keepends=True),
        fromfile="a/" + path, tofile="b/" + path))
    if sha256(ROOT / path) != working_hashes[path]:
        raise RuntimeError("Working file changed during snapshot generation: " + path)
(OUTPUT / "business-logging-only.patch").write_text("".join(patch), encoding="utf-8", newline="\n")
(OUTPUT / "direct-stage.txt").write_text("\n".join(DIRECT_STAGE) + "\n", encoding="utf-8", newline="\n")
mixed = dict(mapping)
for path in DIRECT_STAGE:
    target = OUTPUT / path
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_bytes((ROOT / path).read_bytes())
    if sha256(target) != working_hashes[path]:
        raise RuntimeError("Working file changed during snapshot generation: " + path)
    mapping[path] = str(target)
manifest = {
    "head": EXPECTED_HEAD,
    "files": {path: {"snapshot": target, "source_sha256": working_hashes[path]}
        for path, target in mapping.items()},
    "baseHead": EXPECTED_HEAD,
    "snapshots": mixed,
    "snapshotSha256": {path: sha256(Path(target)) for path, target in mapping.items()},
    "directStage": DIRECT_STAGE,
    "workingSha256": working_hashes,
    "excludedNonLogging": EXCLUDED,
    "note": "仅生成业务日志快照；未操作 Git index/commit，未修改真实工作树源码。",
}
(OUTPUT / "snapshot-map.json").write_text(json.dumps(manifest, ensure_ascii=False, indent=2) + "\n", encoding="utf-8", newline="\n")
print(f"Prepared {len(mixed)} mixed-file snapshots and {len(DIRECT_STAGE)} direct-stage snapshots")
