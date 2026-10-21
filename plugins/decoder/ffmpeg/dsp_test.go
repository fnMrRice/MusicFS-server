package audioffmpeg

import (
	"context"
	"math"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
)

func TestDSPChainExecutesInConfiguredOrder(t *testing.T) {
	// User gain precedes the configured chain. Clipping makes this ordering
	// observable: +6 dB -> -6 dB yields ~0.50, the reverse yields 1.0.
	chain, err := buildDSP(context.Background(), 6, []audio.NodeSpec{{Name: "gain", Version: 1, Params: map[string]float64{"gainDB": -6}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer chain.Close()
	block := audio.Block{Format: audio.Canonical, Samples: []float32{1, -1}, Frames: 1}
	if err := chain.Process(&block); err != nil {
		t.Fatal(err)
	}
	if math.Abs(float64(block.Samples[0])-math.Pow(10, -6.0/20)) > 0.000001 {
		t.Fatalf("unexpected DSP chain ordering: %v", block.Samples)
	}
}

func TestDSPValidationRejectsUnsupportedNodes(t *testing.T) {
	for _, spec := range []audio.NodeSpec{
		{Name: "gain", Version: 2, Params: map[string]float64{"gainDB": 0}},
		{Name: "missing", Version: 1},
		{Name: "gain", Version: 1, Params: map[string]float64{"gainDB": math.NaN()}},
		{Name: "gain", Version: 1, Params: map[string]float64{"gainDB": 0, "unknown": 1}},
	} {
		if err := ValidateDSP([]audio.NodeSpec{spec}, nil); err == nil {
			t.Fatalf("invalid DSP accepted: %+v", spec)
		}
	}
}

func TestDSPIdentityAndConfigImmutability(t *testing.T) {
	config := Config{DSPChain: []audio.NodeSpec{{Name: "gain", Version: 1, Params: map[string]float64{"gainDB": -3}}}}
	before := dspRevision(config, strings.Repeat("a", 64))
	processor := &Processor{config: config}
	copy := processor.DSPChain()
	copy[0].Params["gainDB"] = -9
	if before != dspRevision(config, strings.Repeat("a", 64)) {
		t.Fatal("returned DSP config mutated processor")
	}
	if before == dspRevision(config, strings.Repeat("b", 64)) {
		t.Fatal("DSP revision does not bind worker checksum")
	}
	config.DSPChain[0].Params["gainDB"] = -4
	if before == dspRevision(config, strings.Repeat("a", 64)) {
		t.Fatal("DSP revision does not bind node parameters")
	}
	if dspRevision(Config{}, "") != "gain-v1" {
		t.Fatal("legacy processing revision changed")
	}
	if (*Processor)(nil).HasDSP() {
		t.Fatal("nil processor has DSP")
	}
}
