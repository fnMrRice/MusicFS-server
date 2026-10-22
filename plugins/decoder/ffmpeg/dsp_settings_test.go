package audioffmpeg

import (
	"bytes"
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio/dspworker"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugincontrol"
)

func settingsProcessor() *Processor {
	profiles := []Profile{}
	for _, id := range []string{"mp3-128", "opus-5.1", "opus-7.1"} {
		profile, _ := ProfileByID(id)
		profiles = append(profiles, profile)
	}
	p := &Processor{config: Config{Builtin: true}, Capabilities: Capabilities{Profiles: profiles}, slots: make(chan struct{}, 1), normalizeSlot: make(chan struct{}, 1)}
	p.identity = ProcessingIdentity{WorkerSHA256: strings.Repeat("a", 64), PipelineRevision: dspRevision(p.config, strings.Repeat("a", 64))}
	return p
}

func TestDSPSettingsViewsShareAdmissionAndPreserveSnapshots(t *testing.T) {
	p := settingsProcessor()
	options := p.Defaults()
	options.BufferFrames = 257
	options.Chain = []audio.NodeSpec{{Name: "gain", Version: 1, Params: map[string]float64{"gainDB": -6}}}
	view, err := p.WithSettings(t.Context(), options)
	if err != nil {
		t.Fatal(err)
	}
	options.Chain[0].Params["gainDB"] = -12
	if view.DSPChain()[0].Params["gainDB"] != -6 || p.HasDSP() || view.ProcessingIdentity() == p.ProcessingIdentity() {
		t.Fatal("user settings did not retain an independent identity and snapshot")
	}
	if view.slots != p.slots || view.normalizeSlot != p.normalizeSlot {
		t.Fatal("settings view created an independent processing quota")
	}
	p.slots <- struct{}{}
	err = view.Process(t.Context(), Plan{TaskID: "task", AssetID: "asset", Profile: "mp3-128"}, bytes.NewReader([]byte{1}), 1, io.Discard)
	<-p.slots
	if err == nil || !strings.Contains(err.Error(), "RATE_LIMITED") {
		t.Fatal("settings view bypassed the shared processing quota", err)
	}
	gate := plugincontrol.NewGate(true)
	p.SetGate(gate)
	lease, err := view.AcquireLease(t.Context())
	if err != nil || lease == nil || gate.Active() != 1 {
		t.Fatal("settings view did not follow original gate binding", err)
	}
	defer lease.Close()
	gate.SetAccepting(false, false)
	if admitted, err := view.AcquireLease(t.Context()); err == nil {
		admitted.Close()
		t.Fatal("settings view bypassed disabled plugin")
	}
	inherited, err := view.AcquireLease(lease.Context())
	if err != nil {
		t.Fatal("active session lost its drain lease", err)
	}
	inherited.Close()
	gate.SetAccepting(true, false)
	gate.Rotate()
	next, err := view.WithSettings(t.Context(), view.Defaults())
	if err != nil {
		t.Fatal(err)
	}
	if admitted, err := next.AcquireLease(t.Context()); err == nil {
		admitted.Close()
		t.Fatal("settings view renewed a stale runtime epoch")
	}
	gate.SetAccepting(false, true)
	if lease.Context().Err() == nil {
		t.Fatal("forced disable did not cancel shared admission")
	}
}

func TestDSPSettingsValidateBufferAndNegotiatedOutputWithoutStartingWorkers(t *testing.T) {
	p := settingsProcessor()
	for _, size := range []int{0, 1, 63, 16385} {
		options := p.Defaults()
		options.BufferFrames = size
		if err := p.ValidateSettings(t.Context(), options); err == nil {
			t.Fatalf("invalid buffer size accepted: %d", size)
		}
	}
	for _, size := range []int{64, 257, 4096, 16384} {
		options := p.Defaults()
		options.BufferFrames = size
		options.Chain = []audio.NodeSpec{{Name: "channelmix", Version: 1, Params: map[string]float64{"outputChannels": 8}}, {Name: "reverb", Version: 1}, {Name: "channelmix", Version: 1, Params: map[string]float64{"outputChannels": 2}}}
		if err := p.ValidateSettings(t.Context(), options); err != nil {
			t.Fatal(size, err)
		}
		options.Chain = options.Chain[:2]
		if err := p.ValidateSettings(t.Context(), options); err == nil {
			t.Fatal("DSP output differing from encoder layout was accepted")
		}
	}
	missing, err := filepath.Abs(filepath.Join(t.TempDir(), "does-not-exist.exe"))
	if err != nil {
		t.Fatal(err)
	}
	p.config.DSPPlugins = []dspworker.Config{{Name: "test.external", Executable: missing, SHA256: strings.Repeat("a", 64)}}
	options := p.Defaults()
	options.Chain = []audio.NodeSpec{{Name: "test.external", Version: 1}}
	if err = p.ValidateSettings(t.Context(), options); err != nil {
		t.Fatal("static validation tried to open or launch external worker", err)
	}
	options.BufferFrames = 8192
	if err = p.ValidateSettings(t.Context(), options); err == nil {
		t.Fatal("external v1 worker accepted oversized blocks")
	}
	options.BufferFrames = 4096
	options.OutputLayout = "7.1"
	if err = p.ValidateSettings(t.Context(), options); err == nil {
		t.Fatal("external v1 worker accepted surround input")
	}
}

func TestDSPBufferSizeChangesProcessingIdentity(t *testing.T) {
	p := settingsProcessor()
	first := p.Defaults()
	second := p.Defaults()
	second.BufferFrames = 64
	a, err := p.WithSettings(context.Background(), first)
	if err != nil {
		t.Fatal(err)
	}
	b, err := p.WithSettings(context.Background(), second)
	if err != nil {
		t.Fatal(err)
	}
	if a.ProcessingIdentity() != p.ProcessingIdentity() || a.ProcessingIdentity() == b.ProcessingIdentity() {
		t.Fatal("buffer identity did not normalize deployment default or bind user buffer")
	}
}
