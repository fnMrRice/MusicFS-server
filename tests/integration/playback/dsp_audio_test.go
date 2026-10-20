package playback_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/decoder/ffmpeg"
	"github.com/fnMrRice/MusicFS-server/src/audio"
	"github.com/fnMrRice/MusicFS-server/src/audio/dspworker"
	"github.com/fnMrRice/MusicFS-server/src/playback"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func externalGainWorker(t *testing.T) dspworker.Config {
	t.Helper()
	path := filepath.Join(t.TempDir(), "dsp-gain")
	if runtime.GOOS == "windows" {
		path += ".exe"
	}
	cmd := exec.Command("go", "build", "-mod=readonly", "-o", path, "./plugins/dsp/gain/cmd/musicfs-plugin-dsp-gain")
	cmd.Dir = testkit.RepositoryRoot()
	if raw, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build DSP plugin: %v %s", err, raw)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return dspworker.Config{Name: "musicfs.dsp.gain", Executable: path, SHA256: fmt.Sprintf("%x", sha256.Sum256(raw)), TimeoutMs: 5000}
}

func TestDSPActualPlaybackCacheSeekRecoveryAndFingerprintIsolation(t *testing.T) {
	plain, cfg := cacheAudioProcessor(t)
	f := recoverySetup(t, plain, 0)
	ctx := context.Background()
	base := f.create(t, "dsp-baseline-create")
	baseline, err := processedBytes(t, f, base)
	if err != nil {
		t.Fatal(err)
	}
	cfg.DSPChain = []audio.NodeSpec{
		{Name: "gain", Version: 1, Params: map[string]float64{"gainDB": -3}},
		{Name: "musicfs.dsp.gain", Version: 1, Params: map[string]float64{"gainDB": -6}},
	}
	cfg.DSPPlugins = []dspworker.Config{externalGainWorker(t)}
	dsp, err := audioffmpeg.NewProcessor(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	f.player.UpdateAudio(dsp, nil)
	f.request.Preferences.ForceProcessing = false
	d := f.create(t, "dsp-forced-create")
	if d.DeliveryMode != "processed" || d.SeekMode != "timeRestart" || len(d.EffectiveProcessing) != 3 {
		t.Fatalf("configured DSP was not applied: %+v", d)
	}
	if d.EffectiveProcessing[0].Type != "gain" || d.EffectiveProcessing[0].GainDB != 0 ||
		d.EffectiveProcessing[1].Type != "gain" || d.EffectiveProcessing[1].GainDB != -3 ||
		d.EffectiveProcessing[2].Type != "musicfs.dsp.gain" || d.EffectiveProcessing[2].Parameters["gainDB"] != -6 {
		t.Fatalf("effective DSP order or parameters differ: %+v", d.EffectiveProcessing)
	}
	processed, err := processedBytes(t, f, d)
	if err != nil {
		t.Fatal(err)
	}
	ratio := pcmRMS(t, cfg.FFmpegPath, processed) / pcmRMS(t, cfg.FFmpegPath, baseline)
	if math.Abs(ratio-math.Pow(10, -9.0/20)) > .03 {
		t.Fatalf("external and built-in DSP were not both applied: RMS ratio=%f", ratio)
	}
	records, err := f.repo.ListProcessedCache(ctx)
	if err != nil || len(records) != 2 || records[0].Key == records[1].Key {
		t.Fatalf("different DSP chains did not produce isolated caches: records=%d err=%v", len(records), err)
	}
	profile, err := f.player.RecoveryProfile()
	if err != nil || profile.ProcessingRevision != dsp.ProcessingIdentity().PipelineRevision || !strings.HasPrefix(profile.ProcessingRevision, "dsp-v1:") {
		t.Fatalf("recovery profile did not bind DSP identity: %+v %v", profile, err)
	}
	assertNormalizationUnchanged(t, plain, dsp, f.path)
	wavOnly := f.request
	wavOnly.Capabilities.Formats = []playback.Format{{MIME: "audio/wav", CanPlay: "probably"}}
	_, err = f.player.Create(ctx, f.who, wavOnly, "dsp-no-mpeg-create")
	recoveryError(t, err, "UNSUPPORTED")

	evidence := f.evidence(t, 1)
	aligned := f.create(t, "dsp-aligned-create")
	result, err := f.player.RecoverSource(ctx, f.who, aligned.SessionID, recoveryRequest(aligned, f.tracks[1].ID, &evidence), "dsp-aligned-recovery")
	if err != nil || result.Decision != "continue" || result.Playback == nil {
		t.Fatalf("matching DSP recovery failed: %+v %v", result, err)
	}
	if _, err = processedBytes(t, f, *result.Playback); err != nil {
		t.Fatal(err)
	}

	cfg.DSPChain[1].Params["gainDB"] = -12
	changed, err := audioffmpeg.NewProcessor(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	f.player.UpdateAudio(changed, nil)
	if changed.ProcessingIdentity().PipelineRevision == profile.ProcessingRevision {
		t.Fatal("DSP parameter change retained the previous chain identity")
	}
	seek, err := f.player.Control(ctx, f.who, d.SessionID, "seek", playback.ControlRequest{AttemptID: d.AttemptID, PositionMs: 3000}, "dsp-existing-seek")
	if err != nil || seek.EffectiveProcessing[2].Parameters["gainDB"] != -6 {
		t.Fatalf("existing session did not retain its original DSP: %+v %v", seek, err)
	}
	originalTail, err := processedBytes(t, f, seek)
	if err != nil {
		t.Fatal(err)
	}
	baseSeek, err := f.player.Control(ctx, f.who, base.SessionID, "seek", playback.ControlRequest{AttemptID: base.AttemptID, PositionMs: 3000}, "dsp-baseline-seek")
	if err != nil {
		t.Fatal(err)
	}
	baseTail, err := processedBytes(t, f, baseSeek)
	if err != nil {
		t.Fatal(err)
	}
	ratio = pcmRMS(t, cfg.FFmpegPath, originalTail) / pcmRMS(t, cfg.FFmpegPath, baseTail)
	if math.Abs(ratio-math.Pow(10, -9.0/20)) > .03 {
		t.Fatalf("configuration update changed an existing session's DSP: RMS ratio=%f", ratio)
	}
	result, err = f.player.RecoverSource(ctx, f.who, seek.SessionID, recoveryRequest(seek, f.tracks[1].ID, &evidence), "dsp-changed-recovery")
	if err != nil || result.Decision != "promptRestart" || result.Reason != "OUTPUT_PROFILE_UNAVAILABLE" || result.Playback != nil {
		t.Fatalf("changed DSP chain continued an old alignment: %+v %v", result, err)
	}
}

func pcmRMS(t *testing.T, ffmpeg string, encoded []byte) float64 {
	t.Helper()
	cmd := exec.Command(ffmpeg, "-v", "error", "-i", "pipe:0", "-f", "f32le", "-ac", "1", "-ar", "48000", "pipe:1")
	cmd.Stdin = bytes.NewReader(encoded)
	raw, err := cmd.Output()
	if err != nil || len(raw) < 48000*4 {
		t.Fatalf("decode processed DSP output: %v", err)
	}
	var sum float64
	for i := 0; i+4 <= len(raw); i += 4 {
		v := float64(math.Float32frombits(binary.LittleEndian.Uint32(raw[i:])))
		sum += v * v
	}
	return math.Sqrt(sum / float64(len(raw)/4))
}

func assertNormalizationUnchanged(t *testing.T, plain, dsp *audioffmpeg.Processor, path string) {
	t.Helper()
	input, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil {
		t.Fatal(err)
	}
	plan := audio.NormalizeRequest{TaskID: "dsp-fingerprint-isolation", AssetID: "asset", DurationMs: 10000}
	var baseline, actual bytes.Buffer
	before, err := plain.Normalize(context.Background(), plan, input, info.Size(), &baseline)
	if err != nil {
		t.Fatal(err)
	}
	after, err := dsp.Normalize(context.Background(), plan, input, info.Size(), &actual)
	if err != nil || before != after || !bytes.Equal(baseline.Bytes(), actual.Bytes()) {
		t.Fatalf("playback DSP changed fingerprint normalization: before=%+v after=%+v err=%v", before, after, err)
	}
}
