"""从固定 HEAD 构造配置、接口和文档的日志快照，不修改源码或 Git 索引。"""

from __future__ import annotations

import difflib
import hashlib
import json
from pathlib import Path
import re
import subprocess


ROOT = Path(__file__).resolve().parents[1]
BASE = "f7e9e97a241ca4ed79e154840eab25f9d2a5873d"
OUTPUT = ROOT / "build/logging-commit/config"
MIXED = """
configs/server.example.yaml
configs/server.schema.json
docs/README.md
docs/architecture.md
docs/contracts/http.schema.json
docs/operations/configuration.md
src/core/config/config.go
src/core/config/extensions.go
src/core/config/validate.go
src/core/config/yaml_validation.go
""".split()
PURE = """
docs/contracts/README.md
docs/contracts/openapi.yaml
docs/contracts/web-integration.md
docs/development/dependencies.md
docs/operations/running.md
docs/operations/logging.md
src/app/api/logging.go
src/core/config/builtin.go
src/core/config/document.go
src/core/config/local_directory_defaults.go
src/core/config/paths.go
src/core/config/store.go
src/core/config/logging.go
tests/contract/api/logging_test.go
tests/unit/config/logging_test.go
""".split()


def git(*args: str) -> bytes:
    return subprocess.check_output(["git", *args], cwd=ROOT)


def replace_once(text: str, old: str, new: str) -> str:
    if text.count(old) != 1:
        raise RuntimeError(f"Expected one replacement match: {old[:100]!r}")
    return text.replace(old, new, 1)


def span(text: str, start: str, end: str) -> str:
    if text.count(start) != 1:
        raise RuntimeError(f"Expected one span start: {start!r}")
    first = text.index(start)
    last = text.index(end, first + len(start))
    return text[first:last]


def line(text: str, prefix: str) -> str:
    matches = [part for part in text.splitlines(keepends=True) if part.startswith(prefix)]
    if len(matches) != 1:
        raise RuntimeError(f"Expected one line prefix: {prefix!r}")
    return matches[0]


def normalize(raw: bytes) -> str:
    return raw.decode("utf-8").replace("\r\n", "\n")


def digest(raw: bytes) -> str:
    return hashlib.sha256(raw).hexdigest()


if git("rev-parse", "HEAD").decode().strip() != BASE:
    raise RuntimeError("HEAD changed; review the snapshot base before proceeding")
sources = {name: (ROOT / name).read_bytes() for name in MIXED + PURE}
tracked = set(git("ls-tree", "-r", "--name-only", BASE).decode("utf-8").splitlines())
bases = {
    name: normalize(git("show", f"{BASE}:{name}")) if name in tracked else ""
    for name in MIXED + PURE
}
current = {name: normalize(raw) for name, raw in sources.items()}
result = {name: bases[name] for name in MIXED}

# 仅提取日志块，并将其放回未包含声道及 DSP 新功能的基准内容。
name = "configs/server.example.yaml"
logging_example = span(current[name], "logging:\n", "storage:\n")
result[name] = replace_once(result[name], "storage:\n", logging_example + "storage:\n")

name = "configs/server.schema.json"
logging_schema = span(current[name], '    "logging": {\n', '    "storage": ')
result[name] = replace_once(result[name], '    "storage": ', logging_schema + '    "storage": ')

name = "docs/README.md"
anchor = line(result[name], "| [配置]")
result[name] = replace_once(result[name], anchor, anchor + line(current[name], "| [日志]"))

name = "docs/architecture.md"
result[name] = replace_once(result[name], line(result[name], "| src/core |"), line(current[name], "| src/core |"))
anchor = "## 播放与生命周期\n"
result[name] = replace_once(result[name], anchor, line(current[name], "日志由 `src/core/logging`") + "\n" + anchor)

name = "docs/contracts/http.schema.json"
logging_http = span(current[name], '    "LoggingSettings": {\n', '    "PluginDisplayCount": ')
result[name] = replace_once(result[name], '    "PluginDisplayCount": ', logging_http + '    "PluginDisplayCount": ')

name = "docs/operations/configuration.md"
anchor = line(result[name], "| `webRoot` |")
rows = "".join(line(current[name], f"| `logging.{field}` |") for field in ("directory", "level", "plugins"))
result[name] = replace_once(result[name], anchor, anchor + rows)
result[name] = replace_once(result[name], line(result[name], "优先级为"), line(current[name], "优先级为"))
anchor = line(result[name], "管理员令牌至少")
result[name] = replace_once(result[name], anchor, line(current[name], "`logging` 可省略") + "\n" + anchor)

name = "src/core/config/config.go"
result[name] = replace_once(result[name], "type Config struct {\n", "type Config struct {\n" + line(current[name], "\tLogging "))
anchor = line(result[name], "\t\tHashing:")
result[name] = replace_once(result[name], anchor, line(current[name], "\t\tLogging:") + anchor)

name = "src/core/config/extensions.go"
old_comment = span(result[name], "// SameStartupSettings", "func SameStartupSettings")
new_comment = span(current[name], "// SameStartupSettings", "func SameStartupSettings")
result[name] = replace_once(result[name], old_comment, new_comment)
anchor = "\t\treflect.DeepEqual(a.Storage, b.Storage) && reflect.DeepEqual(a.AudioDSP, b.AudioDSP)\n"
result[name] = replace_once(result[name], anchor, anchor.rstrip("\n") + " &&\n" + line(current[name], "\t\treflect.DeepEqual(a.Logging.normalized"))

name = "src/core/config/validate.go"
anchor = "func (c Config) Validate() error {\n"
logging_check = span(current[name], "\tc.Logging = c.Logging.normalized", "\tif err := c.Storage.Validate()")
result[name] = replace_once(result[name], anchor, anchor + logging_check)
result[name] = replace_once(result[name], "c.validatePlugins(writable)", "c.validatePlugins(append(writable, c.Logging.Directory))")

name = "src/core/config/yaml_validation.go"
anchor = '\t\t"storage": storageShape(), "audioDSP": audioDSPShape(),\n'
result[name] = replace_once(result[name], anchor, anchor.rstrip("\n") + ' "logging": loggingShape(),\n')
anchor = '\tif version := yamlField(root, "configVersion"); version.Value != "1" {\n'
logging_check = "\tif err := validateLoggingNodes(root); err != nil {\n\t\treturn err\n\t}\n"
if logging_check not in current[name]:
    raise RuntimeError("Logging YAML validation block changed")
result[name] = replace_once(result[name], anchor, logging_check + anchor)

# 对结构化协议做严格比对：去掉日志定义后必须与 HEAD 完全一致。
schema = json.loads(result["configs/server.schema.json"])
base_schema = json.loads(bases["configs/server.schema.json"])
assert schema["properties"].pop("logging") == json.loads(current["configs/server.schema.json"])["properties"]["logging"]
assert schema == base_schema
contract = json.loads(result["docs/contracts/http.schema.json"])
base_contract = json.loads(bases["docs/contracts/http.schema.json"])
assert contract["$defs"].pop("LoggingSettings") == json.loads(current["docs/contracts/http.schema.json"])["$defs"]["LoggingSettings"]
assert contract == base_contract
openapi = json.loads(current["docs/contracts/openapi.yaml"])
base_openapi = json.loads(bases["docs/contracts/openapi.yaml"])
assert set(openapi["paths"].pop("/admin/logging")) == {"get", "put"}
assert openapi == base_openapi

OUTPUT.mkdir(parents=True, exist_ok=True)
manifest = {"head": BASE, "files": {}, "mixed_paths": MIXED, "pure_stage_paths": PURE}
patches = []
for name in MIXED + PURE:
    text = result[name] if name in result else current[name]
    # 同时检查纯文件，避免并行任务在已审阅文件中加入声道功能。
    added = [new for op, _, _, start, end in difflib.SequenceMatcher(None, bases[name].splitlines(), text.splitlines()).get_opcodes()
             if op in {"insert", "replace"} for new in text.splitlines()[start:end]]
    unexpected = re.compile(r"audioChannels|AudioChannels|opus-5\.1|opus-7\.1|musicfs\.dsp\.(equalizer|compressor|reverb)|dsp-effects\.md")
    if any(unexpected.search(new) for new in added):
        raise RuntimeError(f"Non-logging audio changes found in {name}")
    snapshot = OUTPUT / name
    snapshot.parent.mkdir(parents=True, exist_ok=True)
    raw = text.encode("utf-8") if name in MIXED else sources[name]
    snapshot.write_bytes(raw)
    manifest["files"][name] = {
        "snapshot": str(snapshot),
        "source_sha256": digest(sources[name]),
        "snapshot_sha256": digest(raw),
    }
    patches.extend(difflib.unified_diff(bases[name].splitlines(keepends=True), text.splitlines(keepends=True), fromfile=f"a/{name}", tofile=f"b/{name}"))

# 在生成结束时确认源码未被脚本或并行工作改动；只格式检查快照。
for name, raw in sources.items():
    if (ROOT / name).read_bytes() != raw:
        raise RuntimeError(f"Source changed during snapshot creation: {name}")
go_files = [entry["snapshot"] for name, entry in manifest["files"].items() if name.endswith(".go")]
unformatted = subprocess.check_output(["gofmt", "-l", *go_files], cwd=ROOT).decode().strip()
if unformatted:
    raise RuntimeError(f"Unformatted snapshot files: {unformatted}")
(OUTPUT / "manifest.json").write_text(json.dumps(manifest, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
(OUTPUT / "logging-only.patch").write_text("".join(patches), encoding="utf-8", newline="\n")
print(f"Prepared {len(manifest['files'])} logging config/API/docs files ({len(MIXED)} isolated, {len(PURE)} pure)")
print("Validated JSON schemas, OpenAPI isolation, Go formatting, source stability, and audio-change exclusion")
