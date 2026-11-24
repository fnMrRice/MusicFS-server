package channelmix

import (
	"math"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
)

func TestNodeDeclaresEveryTargetAndPreservesBlockTiming(t *testing.T) {
	for _, channels := range []int{1, 2, 6, 8} {
		node, err := NewNode(map[string]float64{"outputChannels": float64(channels)})
		if err != nil {
			t.Fatal(err)
		}
		declaration, err := audio.DescribeIO(node, audio.Canonical)
		if err != nil || declaration.Format.Channels != channels || !declaration.CanContinueDSP {
			t.Fatalf("bad output declaration: %+v err=%v", declaration, err)
		}
		if err := node.Process(&audio.Block{Format: audio.Canonical}); err == nil {
			t.Fatal("DescribeIO configured the node")
		}
		if _, err = node.ConfigureIO(audio.Canonical); err != nil {
			t.Fatal(err)
		}
		block := &audio.Block{Format: audio.Canonical, Frames: 1, Samples: []float32{0.75, -0.25}, Sequence: 4, PositionFrames: 800, End: true, Discontinuity: true}
		if err = node.Process(block); err != nil {
			t.Fatal(err)
		}
		if block.Format != declaration.Format || len(block.Samples) != channels || block.Frames != 1 || block.Sequence != 4 || block.PositionFrames != 800 || !block.End || !block.Discontinuity {
			t.Fatalf("node changed timing: %+v", block)
		}
		if channels == 1 && block.Samples[0] != 0.25 {
			t.Fatalf("stereo average was %g", block.Samples[0])
		}
		for _, lifecycle := range []func() error{node.Reset, node.Drain, node.Close} {
			if err = lifecycle(); err != nil {
				t.Fatal(err)
			}
		}
		if node.DelayFrames() != 0 || !node.(interface{ Stateless() bool }).Stateless() {
			t.Fatal("unexpected node processing capabilities")
		}
	}
}

func TestNodeRejectsInvalidParametersAndBlocks(t *testing.T) {
	for _, params := range []map[string]float64{
		{"outputChannels": 0}, {"outputChannels": 3}, {"outputChannels": 5.1}, {"outputChannels": math.NaN()}, {"outputChannels": math.Inf(1)}, {"outputchannels": 2},
	} {
		if _, err := NewNode(params); err == nil {
			t.Fatalf("accepted invalid parameters: %+v", params)
		}
	}
	node, err := NewNode(nil)
	if err != nil {
		t.Fatal(err)
	}
	declaration, err := audio.DescribeIO(node, audio.Canonical)
	if err != nil || declaration.Format != audio.Canonical {
		t.Fatalf("default output changed: %+v %v", declaration, err)
	}
	if _, err = node.ConfigureIO(audio.Canonical); err != nil {
		t.Fatal(err)
	}
	mono, _ := audio.FormatForLayout("mono")
	for _, block := range []*audio.Block{nil, {}, {Format: mono}, {Format: audio.Canonical, Frames: 1, Samples: []float32{1}}, {Format: audio.Canonical, Frames: 1, Samples: []float32{0, float32(math.NaN())}}} {
		if err = node.Process(block); err == nil {
			t.Fatal("accepted invalid block")
		}
	}
}

func TestMonoDownmixPreservesCenterAndExcludesLFE(t *testing.T) {
	mono := sourceFormat("mono")
	for _, input := range []string{"mono", "stereo", "5.1", "5.1(side)", "7.1"} {
		converter, err := New(sourceFormat(input), mono)
		if err != nil {
			t.Fatal(err)
		}
		for channel, label := range layouts[input] {
			samples := make([]float32, len(layouts[input]))
			samples[channel] = 1
			got, err := converter.Process(samples)
			if err != nil || len(got) != 1 {
				t.Fatalf("invalid mono result: %v err=%v", got, err)
			}
			if label == "LFE" && got[0] != 0 || label != "LFE" && got[0] <= 0 {
				t.Fatalf("invalid mono contribution: %s %s %v", input, label, got)
			}
		}
	}
}
