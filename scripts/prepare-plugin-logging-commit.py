"""从固定 HEAD 准备插件日志提交内容，不改工作树源码或 Git 索引。"""

from __future__ import annotations

import hashlib
import json
from pathlib import Path
import subprocess


ROOT = Path(__file__).resolve().parents[1]
BASE = "f7e9e97a241ca4ed79e154840eab25f9d2a5873d"
OUTPUT = ROOT / "build/logging-commit/plugin"


def git(*args: str) -> str:
    return subprocess.check_output(["git", *args], cwd=ROOT).decode("utf-8")


def replace_once(text: str, old: str, new: str) -> str:
    if text.count(old) != 1:
        raise RuntimeError(f"Expected exactly one replacement match: {old[:100]!r}")
    return text.replace(old, new, 1)


def add_import(text: str, anchor: str, package: str) -> str:
    return replace_once(text, f'\t"{anchor}"\n', f'\t"{anchor}"\n\t"{package}"\n')


SDK_LOG = "github.com/fnMrRice/MusicFS-server/sdk/pluginlog"
CORE_LOG = "github.com/fnMrRice/MusicFS-server/src/core/logging"


def pipeline(text: str) -> str:
    text = replace_once(text, '\t"time"\n', f'\t"time"\n\n\t"{SDK_LOG}"\n\t"{CORE_LOG}"\n')
    text = replace_once(text,
        "func Pipeline(ctx context.Context, r StartRequest, output io.Writer) error {",
        "func Pipeline(ctx context.Context, r StartRequest, output io.Writer) (pipelineErr error) {\n"
        '\tfinish := pluginlog.Operation(ctx, "audio.pipeline")\n'
        "\tdefer func() { finish(pipelineErr) }()")
    return replace_once(text,
        "\tdecoder.Stderr = io.Discard\n\tencoder.Stderr = io.Discard",
        '\tdecodeLog := logging.PluginStderr("musicfs.plugin.decoder.ffmpeg", "")\n'
        '\tencodeLog := logging.PluginStderr("musicfs.plugin.decoder.ffmpeg", "")\n'
        "\tdefer decodeLog.Close()\n\tdefer encodeLog.Close()\n"
        "\tdecoder.Stderr, encoder.Stderr = decodeLog, encodeLog")


def processor(text: str) -> str:
    text = add_import(text, "github.com/fnMrRice/MusicFS-server/sdk/instanceid", SDK_LOG)
    text = add_import(text, "github.com/fnMrRice/MusicFS-server/sdk/pluginproto", CORE_LOG)
    text = replace_once(text,
        "func (p *Processor) run(ctx context.Context, taskID string, readerAt io.ReaderAt, size int64, output io.Writer, operation func(*pluginproto.Peer, string, string) error) error {",
        "func (p *Processor) run(ctx context.Context, taskID string, readerAt io.ReaderAt, size int64, output io.Writer, operation func(*pluginproto.Peer, string, string) error) (runErr error) {\n"
        '\tctx = pluginlog.Context(ctx, "musicfs.plugin.decoder.ffmpeg", p.config.InstanceID)\n'
        '\tfinish := pluginlog.Operation(ctx, "audio.process")\n'
        "\tdefer func() { finish(runErr) }()\n"
        '\tpluginlog.Logger(ctx).Log(ctx, pluginlog.LevelTrace, "Audio task admitted", "inputBytes", size, "builtin", p.config.Builtin, "dspNodes", len(p.config.DSPChain))')
    text = replace_once(text, "\t\tcmd.Stderr = io.Discard",
        '\t\tstderr := logging.PluginStderr("musicfs.plugin.decoder.ffmpeg", p.config.InstanceID)\n'
        "\t\tcmd.Stderr = stderr\n"
        "\t\tdefer func() {\n\t\t\tif cmd.Process == nil {\n\t\t\t\t_ = stderr.Close()\n\t\t\t}\n\t\t}()")
    text = replace_once(text,
        '\t\t\tcmd.Wait()\n\t\t\treturn fmt.Errorf("UNSUPPORTED: process cleanup containment unavailable")',
        '\t\t\tcmd.Wait()\n\t\t\tstderr.Close()\n\t\t\treturn fmt.Errorf("UNSUPPORTED: process cleanup containment unavailable")')
    return replace_once(text, "go func() { cmd.Wait(); peer.Close(); close(done) }()", "go func() { cmd.Wait(); stderr.Close(); peer.Close(); close(done) }()")


def worker(text: str) -> str:
    text = add_import(text, "github.com/fnMrRice/MusicFS-server/sdk/instanceid", SDK_LOG)
    return replace_once(text, "\ttaskID := w.taskID\n", '\ttaskID := w.taskID\n\tctx = pluginlog.Context(ctx, "musicfs.plugin.decoder.ffmpeg", w.instanceID)\n')


def gain(text: str) -> str:
    text = replace_once(text, "import (\n", 'import (\n\t"context"\n')
    text = replace_once(text, '\t"fmt"\n', '\t"fmt"\n\t"log/slog"\n')
    text = replace_once(text, '\t"math"\n', f'\t"math"\n\n\t"{SDK_LOG}"\n')
    text = replace_once(text, "\tconfigured bool\n}", "\tconfigured bool\n\tlogger *slog.Logger\n}\n\n"
        "func (g *Gain) log() *slog.Logger {\n"
        "\tif g.logger != nil {\n\t\treturn g.logger\n\t}\n"
        '\treturn slog.Default().With("pluginId", "musicfs.dsp.gain")\n}')
    text = replace_once(text, "\treturn &Gain{multiplier: float32(math.Pow(10, db/20))}, nil",
        '\tlogger := slog.Default().With("pluginId", "musicfs.dsp.gain")\n'
        '\tlogger.Debug("DSP gain node created")\n'
        "\treturn &Gain{multiplier: float32(math.Pow(10, db/20)), logger: logger}, nil")
    text = replace_once(text, "\tg.configured = true\n", '\tg.configured = true\n\tg.log().Debug("DSP gain node configured")\n')
    text = replace_once(text, "func (g *Gain) Process(block *audio.Block) error {", "func (g *Gain) Process(block *audio.Block) (processErr error) {\n"
        "\tdefer func() {\n\t\tif processErr != nil {\n"
        '\t\t\tg.log().Warn("DSP gain block failed", "errorCode", pluginlog.ErrorCode(processErr))\n'
        "\t\t}\n\t}()")
    text = replace_once(text, "\t\tblock.Samples[i] = max(-1, min(1, v))\n\t}\n", "\t\tblock.Samples[i] = max(-1, min(1, v))\n\t}\n"
        '\tg.log().Log(context.Background(), pluginlog.LevelTrace, "DSP gain block processed", "frames", block.Frames, "sequence", block.Sequence)\n')
    return replace_once(text, "func (g *Gain) Close() error       { g.configured = false; return nil }", "func (g *Gain) Close() error {\n\tg.configured = false\n"
        '\tg.log().Debug("DSP gain node closed")\n\treturn nil\n}')


PURE = """
plugins/analysis/chromaprint/extractor.go
plugins/database/sqlite/catalog_queries.go
plugins/database/sqlite/cmd/musicfs-plugin-storage-sqlite/main.go
plugins/database/sqlite/hash_lifecycle.go
plugins/database/sqlite/jobs.go
plugins/database/sqlite/processed_cache.go
plugins/database/sqlite/scan_job_state.go
plugins/database/sqlite/state/store.go
plugins/database/sqlite/store.go
plugins/database/sqlite/writeback_commit.go
plugins/decoder/ffmpeg/cmd/musicfs-plugin-audio-ffmpeg/main.go
plugins/decoder/ffmpeg/metadata.go
plugins/decoder/ffmpeg/normalize.go
plugins/decoder/ncm/cmd/musicfs-plugin-ncm/main.go
plugins/decoder/ncm/processor.go
plugins/dsp/gain/cmd/musicfs-plugin-dsp-gain/main.go
plugins/matching/standard/find.go
plugins/scraper/local/cmd/musicfs-plugin-scraper-local/main.go
plugins/scraper/local/engine.go
plugins/scraper/local/host.go
plugins/source/localfiles/cmd/musicfs-plugin-local/main.go
plugins/source/localfiles/scan.go
plugins/source/localfiles/worker.go
plugins/source/online/cmd/musicfs-plugin-applemusic/main.go
plugins/source/online/cmd/musicfs-plugin-netease/main.go
plugins/source/online/cmd/musicfs-plugin-online/main.go
plugins/source/online/cmd/musicfs-plugin-qqmusic/main.go
plugins/source/online/internal/adapter/account_http.go
plugins/tags/flac/flac.go
plugins/transform/thumbnail/thumbnail.go
sdk/pluginlog/log.go
sdk/pluginlog/log_test.go
sdk/pluginproto/peer.go
sdk/provider/worker.go
src/playback/audio/dspworker/process.go
src/plugins/plugincontrol/reconcile.go
src/plugins/pluginhost/online_process.go
src/plugins/pluginhost/process.go
src/plugins/pluginrpc/endpoint.go
src/plugins/pluginrpc/process.go
tests/integration/plugins/logging_test.go
""".strip().splitlines()

DEFERRED = {
    "plugins/decoder/ffmpeg/dsp.go": "仅有未提交 DSP / 声道功能改动，无本次日志差异。",
    "plugins/decoder/ffmpeg/capabilities.go": "输出格式探测属于未提交声道功能。",
    "plugins/decoder/ffmpeg/channel_reader.go": "未提交声道功能入口，含日志包装接线，整体随 DSP 功能提交。",
    "plugins/decoder/ffmpeg/channel_logging.go": "依赖尚未提交的 channelmix，随 DSP 功能提交。",
    "plugins/decoder/ffmpeg/channel_logging_test.go": "依赖尚未提交的 channelmix/WAV 测试工具。",
    "src/playback/audio/nodes.go": "新增效果的公共日志包装接线，整体随 DSP 功能提交。",
    "src/playback/audio/logging.go": "仅服务尚未提交的 equalizer/compressor/reverb，随 DSP 功能提交。",
    "tests/unit/audio/effects_logging_test.go": "依赖尚未提交的 builtin/effects。",
    "src/playback/audio/dspworker/config.go": "未提交的内置效果名称校验改动，无日志差异。",
    "src/playback/audio/dspworker/protocol.go": "未提交的 DSP 格式/协议改动，无日志差异。",
}


def sha(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def main() -> None:
    if git("rev-parse", "HEAD").strip() != BASE:
        raise RuntimeError("HEAD changed; review the base before preparing snapshots")
    OUTPUT.mkdir(parents=True, exist_ok=True)
    mixed = {
        "plugins/decoder/ffmpeg/pipeline.go": pipeline,
        "plugins/decoder/ffmpeg/processor.go": processor,
        "plugins/decoder/ffmpeg/worker.go": worker,
        "plugins/dsp/gain/gain.go": gain,
    }
    before = {path: sha(ROOT / path) for path in [*mixed, *PURE]}
    prepared = []
    for path, transform in mixed.items():
        source = git("show", f"{BASE}:{path}")
        target = OUTPUT / "mixed" / path
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_text(transform(source), encoding="utf-8", newline="\n")
        subprocess.run(["gofmt", "-w", str(target)], check=True, cwd=ROOT)
        prepared.append({"path": path, "source": str(target), "sha256": sha(target), "worktreeSha256": before[path]})
    direct = []
    files = {item["path"]: {"snapshot": item["source"], "source_sha256": item["worktreeSha256"]} for item in prepared}
    for path in PURE:
        target = OUTPUT / "pure" / path
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_bytes((ROOT / path).read_bytes())
        direct.append({"path": path, "source": str(ROOT / path), "snapshot": str(target), "sha256": before[path]})
        files[path] = {"snapshot": str(target), "source_sha256": before[path]}
    if any(sha(ROOT / path) != digest for path, digest in before.items()):
        raise RuntimeError("Working source changed during preparation")
    mapping = {"base": BASE, "mixedFiles": prepared, "directStage": direct, "deferToDSPCommit": DEFERRED,
        "requires": ["src/core/logging (主代理准备)", "go.mod/go.sum 中日志依赖 (主代理准备)"]}
    (OUTPUT / "mapping.json").write_text(json.dumps(mapping, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    (OUTPUT / "manifest.json").write_text(json.dumps({"head": BASE, "files": files}, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    (OUTPUT / "direct-stage.txt").write_text("\n".join(PURE) + "\n", encoding="utf-8")
    print(f"Prepared {len(prepared)} mixed-file snapshots and {len(direct)} direct-stage paths; source files and Git index unchanged")


if __name__ == "__main__":
    main()
