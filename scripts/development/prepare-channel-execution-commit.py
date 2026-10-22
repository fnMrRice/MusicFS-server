"""从 HEAD 生成本次声道处理的独立索引，保留并行任务的工作区和暂存内容。"""

import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile

ROOT = Path(__file__).resolve().parents[2]
EXPECTED_HEAD = "d1352187555ea0d18ed4a6c1c8541be6becfc2f6"

FULL = """
docs/playback/audio-processing.md
docs/playback/local-dsp.md
docs/plugins/audio-codecs.md
docs/plugins/dsp-effects.md
docs/plugins/dsp-plugins.md
docs/plugins/dsp-settings.md
plugins/decoder/ffmpeg/channel_execution.go
plugins/decoder/ffmpeg/channel_execution_test.go
plugins/decoder/ffmpeg/dsp.go
plugins/decoder/ffmpeg/dsp_settings.go
plugins/decoder/ffmpeg/effects_execution.go
plugins/decoder/ffmpeg/output_metadata.go
plugins/decoder/ffmpeg/pipeline.go
plugins/decoder/ffmpeg/processor.go
plugins/decoder/ffmpeg/profiles.go
plugins/decoder/ffmpeg/worker.go
plugins/decoder/ffmpeg/worker_version_test.go
scripts/development/update-channel-execution-contracts.py
scripts/development/update-playback-bitrate-contracts.py
sdk/bundledplugin/releases.go
src/app/bootstrap/audio_management.go
src/app/bootstrap/composition.go
src/app/bootstrap/plugins.go
src/core/config/audio_channels.go
src/playback/dsp_settings.go
src/playback/prepare.go
src/playback/service.go
src/playback/source_channels.go
src/playback/timeline.go
src/plugins/dspsettings/catalog.go
src/plugins/dspsettings/models.go
src/plugins/dspsettings/service.go
src/plugins/dspsettings/validation.go
src/plugins/plugincontrol/builtin.go
tests/contract/api/channel_profiles_test.go
tests/contract/plugins/dsp_test.go
tests/e2e/yaml_channel_disabled_test.go
tests/e2e/yaml_surround_test.go
tests/integration/dspsettings/channel_disabled_test.go
tests/integration/playback/client_channels_test.go
tests/integration/playback/client_dsp_test.go
tests/integration/playback/flac_original_test.go
tests/unit/config/audio_channels_test.go
tests/unit/plugincontrol/channelmix_test.go
""".split()


def git(*args, data=None, env=None):
    result = subprocess.run(["git", *args], cwd=ROOT, input=data, env=env,
                            stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    if result.returncode:
        raise RuntimeError(result.stderr.decode(errors="replace"))
    return result.stdout


def head(path):
    return git("show", "HEAD:" + path).decode("utf-8")


def working(path):
    return (ROOT / path).read_text(encoding="utf-8")


def json_span(text, name):
    marker = '    "' + name + '": '
    assert text.count(marker) == 1, name
    start = text.index(marker) + len(marker)
    _, length = json.JSONDecoder().raw_decode(text[start:])
    return start, start + length


def selected_definitions(path, names):
    base, current = head(path), working(path)
    for name in names:
        start, end = json_span(base, name)
        first, last = json_span(current, name)
        base = base[:start] + current[first:last] + base[end:]
    json.loads(base)
    return base


def selected_lines(path, prefixes):
    base, current = head(path), working(path)
    for prefix in prefixes:
        before = [line for line in base.splitlines() if line.startswith(prefix)]
        after = [line for line in current.splitlines() if line.startswith(prefix)]
        assert len(before) == len(after) == 1, (path, prefix)
        base = base.replace(before[0], after[0])
    return base


def prepare():
    assert git("rev-parse", "HEAD").decode().strip() == EXPECTED_HEAD
    (ROOT / ".cache").mkdir(exist_ok=True)
    folder = Path(tempfile.mkdtemp(prefix="channel-commit-", dir=ROOT / ".cache"))
    index = folder / "index"
    env = os.environ | {"GIT_INDEX_FILE": str(index)}
    original_index = git("ls-files", "--stage", "-z")
    git("read-tree", "HEAD", env=env)
    content = {path: working(path) for path in FULL}
    path = "configs/server.example.yaml"
    base, current = head(path), working(path)
    start, end = base.index("audioDSP:\n"), base.index("webRoot:")
    content[path] = base[:start] + current[current.index("audioDSP:\n"):current.index("webRoot:")] + base[end:]
    path = "deploy/server.container.example.yaml"
    base = head(path)
    assert base.count("  plugins: []\n") == 1
    content[path] = base.replace("  plugins: []\n", "  plugins: []\naudioChannels:\n  enabled: false\n")
    path = "configs/server.schema.json"
    base = selected_definitions(path, ["audioChannels"])
    marker = '\n  "allOf": ['
    before, after = base.split(marker, 1)
    original_condition = json.loads(head(path))["allOf"][0]["if"]
    selected_condition = json.loads(working(path))["allOf"][0]["if"]
    start = after.index('      "if": ') + len('      "if": ')
    parsed, length = json.JSONDecoder().raw_decode(after[start:])
    assert parsed == original_condition
    after = after[:start] + json.dumps(selected_condition, ensure_ascii=False) + after[start + length:]
    content[path] = before + marker + after
    content["docs/contracts/http.schema.json"] = selected_definitions(
        "docs/contracts/http.schema.json", ["TimelineProfile", "Preferences", "OutputProfile", "Playback"])
    content["docs/contracts/plugins/v1/protocol.schema.json"] = selected_definitions(
        "docs/contracts/plugins/v1/protocol.schema.json", ["AudioProfile", "AudioPlan", "AudioStart", "AudioPrepared"])
    content["docs/contracts/openapi.yaml"] = selected_definitions("docs/contracts/openapi.yaml", ["/playback/sessions"])
    content["docs/contracts/plugins/v1/README.md"] = selected_lines(
        "docs/contracts/plugins/v1/README.md", ["`audio.prepare/start`", "官方 FFmpeg Worker"])
    content["docs/operations/configuration.md"] = selected_lines("docs/operations/configuration.md", ["顶层 `storage`"])
    for path, text in content.items():
        digest = git("hash-object", "-w", "--stdin", data=text.encode()).decode().strip()
        git("update-index", "--add", "--cacheinfo", "100644," + digest + "," + path, env=env)
    git("diff", "--cached", "--check", env=env)
    assert git("ls-files", "--stage", "-z") == original_index
    snapshot = folder / "snapshot"
    snapshot.mkdir()
    git("checkout-index", "--all", "--prefix=" + snapshot.as_posix() + "/", env=env)
    state = {"base": EXPECTED_HEAD, "index": str(index), "snapshot": str(snapshot),
             "paths": sorted(content), "indexBefore": original_index.decode(),
             "workingHashes": {p: hashlib.sha256((ROOT / p).read_bytes()).hexdigest() for p in content}}
    (folder / "state.json").write_text(json.dumps(state, ensure_ascii=False, indent=2), encoding="utf-8")
    print(json.dumps({"state": str(folder / "state.json"), "index": str(index), "snapshot": str(snapshot), "files": len(content)}))


def finish(state_path):
    state = json.loads(Path(state_path).read_text(encoding="utf-8"))
    assert git("rev-parse", "HEAD").decode().strip() == state["base"], "HEAD changed"
    assert git("ls-files", "--stage", "-z").decode() == state["indexBefore"], "Real index changed"
    for path, digest in state["workingHashes"].items():
        assert hashlib.sha256((ROOT / path).read_bytes()).hexdigest() == digest, path + " changed"
    env = os.environ | {"GIT_INDEX_FILE": state["index"]}
    git("diff", "--cached", "--check", env=env)
    git("-c", "commit.gpgsign=false", "commit", "-m", "禁用服务端声道插件并保留客户端来源声道", env=env)
    git("restore", "--staged", "--source=HEAD", "--", *state["paths"])
    changed = set(state["paths"])
    def unrelated(raw):
        return [entry for entry in raw.split("\0") if entry and entry.split("\t", 1)[1] not in changed]
    assert unrelated(git("ls-files", "--stage", "-z").decode()) == unrelated(state["indexBefore"])
    for path, digest in state["workingHashes"].items():
        assert hashlib.sha256((ROOT / path).read_bytes()).hexdigest() == digest, path + " changed"
    print(git("rev-parse", "HEAD").decode().strip())


if __name__ == "__main__":
    if len(sys.argv) == 1:
        prepare()
    else:
        finish(sys.argv[1])
