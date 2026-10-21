package audioffmpeg

import (
	"math"
	"os"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio/dspworker"
)

func TestMultichannelDSPBuiltinGainAndEffects(t *testing.T) {
	for _, layout := range []string{"5.1", "7.1"} {
		format, _ := audio.FormatForLayout(layout)
		chain, err := buildDSPFormat(t.Context(), 0, []audio.NodeSpec{
			{Name: "musicfs.dsp.gain", Version: 1, Params: map[string]float64{"gainDB": -6}},
			{Name: "equalizer", Version: 1},
			{Name: "compressor", Version: 1, Params: map[string]float64{"ratio": 1}},
			{Name: "reverb", Version: 1, Params: map[string]float64{"wet": 0}},
		}, []dspworker.Config{{Name: "musicfs.dsp.gain", Builtin: true}}, format)
		if err != nil {
			t.Fatal(err)
		}
		samples := make([]float32, format.Channels)
		for i := range samples {
			samples[i] = .2
		}
		block := audio.Block{Format: format, Samples: samples, Frames: 1}
		if err := chain.Process(&block); err != nil {
			t.Fatal(err)
		}
		chain.Close()
		for _, sample := range samples {
			if math.Abs(float64(sample)-.2*math.Pow(10, -.3)) > 1e-6 {
				t.Fatalf("builtin gain or effects changed multichannel routing: %v", samples)
			}
		}
	}
}

func TestMultichannelDSPRejectsExternalV1BeforeExecution(t *testing.T) {
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	format, _ := audio.FormatForLayout("7.1")
	_, err = buildDSPFormat(t.Context(), 0, []audio.NodeSpec{{Name: "vendor.effect", Version: 1}},
		[]dspworker.Config{{Name: "vendor.effect", Executable: path, SHA256: strings.Repeat("a", 64)}}, format)
	if err == nil || !strings.Contains(err.Error(), "requires stereo output") {
		t.Fatalf("external v1 used a multichannel format: %v", err)
	}
}

func TestOutputLayoutChangesDSPIdentity(t *testing.T) {
	base := dspRevision(Config{}, "worker")
	if base != dspRevision(Config{OutputLayout: "stereo"}, "worker") {
		t.Fatal("default layout is not canonicalized")
	}
	if base == dspRevision(Config{OutputLayout: "5.1"}, "worker") || dspRevision(Config{OutputLayout: "5.1"}, "worker") == dspRevision(Config{OutputLayout: "7.1"}, "worker") {
		t.Fatal("different channel layouts share processing identity")
	}
}
