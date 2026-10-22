"""生成日志提交中宿主基础设施的独立快照，不修改工作树或索引。"""

import hashlib
import json
import subprocess
from pathlib import Path

root = Path(__file__).resolve().parents[1]
target = root / "build/logging-commit/root"
target.mkdir(parents=True, exist_ok=True)
head = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=root, text=True).strip()

paths = [
    "go.mod", "go.sum", "src/app/api/auth.go", "src/app/api/server.go",
    "src/app/bootstrap/lifecycle.go", "src/app/bootstrap/run.go", "src/app/bootstrap/server.go",
    "src/app/musicfs-plugins/main.go", "tests/integration/bootstrap/logging_test.go",
    "tests/integration/bootstrap/managed_logging_test.go",
]
for directory in ("src/core/logging", "tests/unit/logging"):
    paths.extend(path.relative_to(root).as_posix() for path in (root / directory).glob("*.go"))

manifest = {"head": head, "files": {}}
for name in paths:
    content = (root / name).read_bytes()
    output = target / name
    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_bytes(content)
    manifest["files"][name] = {"snapshot": str(output), "source_sha256": hashlib.sha256(content).hexdigest()}

name = "src/app/bootstrap/logging.go"
source = (root / name).read_bytes()
content = source.decode("utf-8").replace("\r\n", "\n")
line = '\t\t"musicfs.dsp.equalizer", "musicfs.dsp.compressor", "musicfs.dsp.reverb", "musicfs.plugin.decoder.channelmix",\n'
assert content.count(line) == 1
content = content.replace(line, "")
output = target / name
output.parent.mkdir(parents=True, exist_ok=True)
output.write_text(content, encoding="utf-8", newline="\n")
manifest["files"][name] = {"snapshot": str(output), "source_sha256": hashlib.sha256(source).hexdigest()}
(target / "manifest.json").write_text(json.dumps(manifest, ensure_ascii=False, indent=2), encoding="utf-8")
print(f"Prepared {len(manifest['files'])} host logging files")

parent = root.parent
parent_head = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=parent, text=True).strip()
name = "docs/architecture.md"
source = (parent / name).read_bytes()
paragraph = next(line for line in source.decode("utf-8").splitlines() if line.startswith("Server 使用结构化日志，"))
content = subprocess.check_output(["git", "show", "HEAD:" + name], cwd=parent).decode("utf-8")
anchor = "当前 Server / 插件 / 最小 Web 的交付顺序与联合验收见"
assert content.count(anchor) == 1 and paragraph not in content
content = content.replace(anchor, paragraph + "\n\n" + anchor)
output = target / "parent-architecture.md"
output.write_text(content, encoding="utf-8", newline="\n")
parent_manifest = {"head": parent_head, "path": name, "snapshot": str(output), "source_sha256": hashlib.sha256(source).hexdigest()}
(target / "parent-manifest.json").write_text(json.dumps(parent_manifest, ensure_ascii=False, indent=2), encoding="utf-8")
