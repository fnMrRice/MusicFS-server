package dsp_test

import (
	"math"
	"slices"
	"testing"

	dsp "github.com/fnMrRice/MusicFS-server/plugins/dsp/gain"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
)

func TestGainSupportsConfiguredChannelLayouts(t *testing.T) {
	for _, layout := range []string{"mono", "stereo", "5.1", "7.1"} {
		format, _ := audio.FormatForLayout(layout)
		for _, db := range []float64{-96, -6, 0, 12} {
			node, err := dsp.NewGain(db)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := node.ConfigureIO(format); err != nil {
				t.Fatal(err)
			}
			input := make([]float32, format.Channels*4)
			for i := range input {
				input[i] = float32(i%7-3) * 0.2
			}
			block := audio.Block{Format: format, Frames: 4, Samples: slices.Clone(input), Sequence: 7, PositionFrames: 99, Discontinuity: true, End: true}
			if err := node.Process(&block); err != nil {
				t.Fatal(err)
			}
			multiplier := float32(math.Pow(10, db/20))
			for i, sample := range block.Samples {
				if want := max(-1, min(1, input[i]*multiplier)); sample != want {
					t.Fatalf("%s gain %g channel sample %d: %g != %g", layout, db, i, sample, want)
				}
			}
			if block.Format != format || block.Frames != 4 || block.Sequence != 7 || block.PositionFrames != 99 || !block.Discontinuity || !block.End {
				t.Fatal("gain changed block metadata")
			}
			if !node.(interface{ Stateless() bool }).Stateless() || node.DelayFrames() != 0 {
				t.Fatal("gain capabilities changed")
			}
			if err := node.Close(); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestGainRejectsUnconfiguredFormatChangesAndInvalidSamples(t *testing.T) {
	node, err := dsp.NewGain(-6)
	if err != nil {
		t.Fatal(err)
	}
	defer node.Close()
	if err := node.Process(nil); err == nil {
		t.Fatal("processed unconfigured node")
	}
	for _, layout := range []string{"7.1", "mono", "5.1", "stereo"} {
		format, _ := audio.FormatForLayout(layout)
		if _, err := node.ConfigureIO(format); err != nil {
			t.Fatal(err)
		}
		if err := node.Process(nil); err == nil {
			t.Fatal("accepted nil block")
		}
		wrong := audio.Canonical
		if wrong == format {
			wrong, _ = audio.FormatForLayout("mono")
		}
		if err := node.Process(&audio.Block{Format: wrong, Frames: 1, Samples: make([]float32, wrong.Channels)}); err == nil {
			t.Fatal("accepted layout change without configure")
		}
		for _, invalid := range []float32{float32(math.NaN()), float32(math.Inf(1)), float32(math.Inf(-1))} {
			block := audio.Block{Format: format, Frames: 2, Samples: make([]float32, 2*format.Channels)}
			block.Samples[0], block.Samples[len(block.Samples)-1] = 0.5, invalid
			if err := node.Process(&block); err == nil {
				t.Fatal("accepted invalid sample")
			}
			if block.Samples[0] != 0.5 {
				t.Fatal("gain partially changed rejected input")
			}
		}
		if err := node.Reset(); err != nil {
			t.Fatal(err)
		}
		if err := node.Process(&audio.Block{Format: format}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := node.ConfigureIO(audio.Format{}); err == nil {
		t.Fatal("accepted unsupported format")
	}
	if err := node.Close(); err != nil {
		t.Fatal(err)
	}
	if err := node.Process(&audio.Block{Format: audio.Canonical}); err == nil {
		t.Fatal("processed closed node")
	}
}
