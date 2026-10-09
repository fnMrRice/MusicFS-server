package plugins_test

import (
	"math"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/audio"
	"github.com/fnMrRice/MusicFS-server/src/audio/dsp"
)

func TestGainChangesPCMAndValidatesFormat(t *testing.T) {
	node, err := dsp.NewGain(-6.020599913)
	if err != nil {
		t.Fatal(err)
	}
	if err = node.Configure(audio.Canonical); err != nil {
		t.Fatal(err)
	}
	block := audio.Block{Format: audio.Canonical, Samples: []float32{.25, -.25, .5, -.5}, Frames: 2}
	if err = node.Process(&block); err != nil {
		t.Fatal(err)
	}
	for i, want := range []float32{.125, -.125, .25, -.25} {
		if math.Abs(float64(block.Samples[i]-want)) > 1e-6 {
			t.Fatalf("gain mismatch %v", block.Samples)
		}
	}
	node.Reset()
	node.Drain()
	node.Close()
	if node.Process(&block) == nil {
		t.Fatal("closed node accepted PCM")
	}
	for _, db := range []float64{math.NaN(), math.Inf(1), 13, -97} {
		if _, err = dsp.NewGain(db); err == nil {
			t.Fatalf("invalid gain accepted: %v", db)
		}
	}
}
