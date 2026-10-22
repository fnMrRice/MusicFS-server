package effects_test

import (
	"math"
	"slices"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/dsp/effects"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
)

var factories = []struct {
	name   string
	create audio.Factory
	params map[string]float64
}{
	{"equalizer", effects.NewEqualizer, map[string]float64{"gainDB": 8}},
	{"compressor", effects.NewCompressor, nil},
	{"reverb", effects.NewReverb, nil},
}

func configured(t *testing.T, create audio.Factory, params map[string]float64) audio.Node {
	t.Helper()
	node, err := create(params)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = node.Close() })
	if err := node.Configure(audio.Canonical); err != nil {
		t.Fatal(err)
	}
	return node
}

func render(t *testing.T, node audio.Node, input []float32, chunk int, discontinuity bool) []float32 {
	t.Helper()
	result := slices.Clone(input)
	for start := 0; start < len(result); start += chunk * 2 {
		end := min(start+chunk*2, len(result))
		block := audio.Block{Format: audio.Canonical, Samples: result[start:end], Frames: (end - start) / 2,
			Sequence: uint64(start / 2), PositionFrames: int64(start / 2), End: end == len(result),
			Discontinuity: discontinuity && start == 0}
		before := block
		if err := node.Process(&block); err != nil {
			t.Fatal(err)
		}
		if block.Format != before.Format || block.Frames != before.Frames || block.Sequence != before.Sequence ||
			block.PositionFrames != before.PositionFrames || block.End != before.End || block.Discontinuity != before.Discontinuity {
			t.Fatal("effect changed block metadata")
		}
	}
	return result
}

func signal(frames int) []float32 {
	values := make([]float32, frames*2)
	for frame := range frames {
		values[2*frame] = float32(0.15*math.Sin(float64(frame)*0.137) + 0.07*math.Sin(float64(frame)*0.031))
		values[2*frame+1] = float32(0.08 * math.Cos(float64(frame)*0.213))
	}
	return values
}

func TestStreamingStateLifecycleAndIsolation(t *testing.T) {
	input, prefix := signal(20000), signal(2400)
	for _, factory := range factories {
		t.Run(factory.name, func(t *testing.T) {
			baseline := render(t, configured(t, factory.create, factory.params), input, 4096, false)
			for _, chunk := range []int{1, 64, 257, 4096, audio.MaxBlockFrames} {
				node := configured(t, factory.create, factory.params)
				if node.DelayFrames() != 0 || node.(interface{ Stateless() bool }).Stateless() {
					t.Fatal("invalid effect capabilities")
				}
				if got := render(t, node, input, chunk, false); !slices.Equal(got, baseline) {
					t.Fatalf("output depends on block size %d", chunk)
				}
			}
			for _, action := range []string{"reset", "configure", "discontinuity", "close-configure"} {
				node := configured(t, factory.create, factory.params)
				render(t, node, prefix, 251, false)
				switch action {
				case "reset":
					if err := node.Reset(); err != nil {
						t.Fatal(err)
					}
				case "configure":
					if err := node.Configure(audio.Canonical); err != nil {
						t.Fatal(err)
					}
				case "close-configure":
					if err := node.Close(); err != nil {
						t.Fatal(err)
					}
					if err := node.Configure(audio.Canonical); err != nil {
						t.Fatal(err)
					}
				}
				if got := render(t, node, input, 383, action == "discontinuity"); !slices.Equal(got, baseline) {
					t.Fatalf("%s did not clear state", action)
				}
			}
			first := configured(t, factory.create, factory.params)
			second := configured(t, factory.create, factory.params)
			part := render(t, first, input[:3000], 137, false)
			render(t, second, prefix, 211, false)
			part = append(part, render(t, first, input[3000:], 137, false)...)
			if !slices.Equal(part, baseline) {
				t.Fatal("instances share processing state")
			}
			if err := first.Drain(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestInvalidBlocksDoNotMutateInputOrState(t *testing.T) {
	for _, factory := range factories {
		t.Run(factory.name, func(t *testing.T) {
			for _, invalid := range []float32{float32(math.NaN()), float32(math.Inf(1)), float32(math.Inf(-1))} {
				node := configured(t, factory.create, factory.params)
				bad := audio.Block{Format: audio.Canonical, Frames: 2, Samples: []float32{0.5, 0.25, 0.1, invalid}}
				if err := node.Process(&bad); err == nil {
					t.Fatal("accepted non-finite sample")
				}
				if !slices.Equal(bad.Samples[:3], []float32{0.5, 0.25, 0.1}) {
					t.Fatal("partially modified rejected block")
				}
				input := signal(4000)
				expected := render(t, configured(t, factory.create, factory.params), input, 4096, false)
				if got := render(t, node, input, 4096, false); !slices.Equal(got, expected) {
					t.Fatal("rejected block changed state")
				}
			}
			node, err := factory.create(factory.params)
			if err != nil {
				t.Fatal(err)
			}
			if err := node.Process(nil); err == nil {
				t.Fatal("processed before configure")
			}
			if err := node.Reset(); err != nil {
				t.Fatal(err)
			}
			if err := node.Configure(audio.Format{}); err == nil {
				t.Fatal("accepted unsupported format")
			}
			if err := node.Configure(audio.Canonical); err != nil {
				t.Fatal(err)
			}
			for _, block := range []*audio.Block{nil, {}, {Format: audio.Canonical, Frames: 1}, {Format: audio.Canonical, Frames: audio.MaxBlockFrames + 1, Samples: make([]float32, (audio.MaxBlockFrames+1)*2)}} {
				if err := node.Process(block); err == nil {
					t.Fatal("accepted invalid block")
				}
			}
			if err := node.Process(&audio.Block{Format: audio.Canonical}); err != nil {
				t.Fatal(err)
			}
			if err := node.Close(); err != nil {
				t.Fatal(err)
			}
			if err := node.Process(&audio.Block{Format: audio.Canonical}); err == nil {
				t.Fatal("processed after close")
			}
		})
	}
}

func TestParameterValidationAndRegistry(t *testing.T) {
	ranges := []map[string][2]float64{
		{"frequencyHz": {20, 20000}, "q": {0.1, 20}, "gainDB": {-24, 24}},
		{"thresholdDB": {-60, 0}, "ratio": {1, 20}, "attackMs": {0.1, 200}, "releaseMs": {10, 3000}, "makeupDB": {0, 24}, "kneeDB": {0, 24}},
		{"roomSize": {0, 1}, "damping": {0, 1}, "wet": {0, 1}},
	}
	registry := audio.NewRegistry()
	if err := effects.Register(registry); err != nil {
		t.Fatal(err)
	}
	if err := effects.Register(registry); err == nil {
		t.Fatal("accepted duplicate registration")
	}
	for i, factory := range factories {
		t.Run(factory.name, func(t *testing.T) {
			if _, err := registry.Create(factory.name, nil); err != nil {
				t.Fatal(err)
			}
			if _, err := factory.create(map[string]float64{"unknown": 0}); err == nil {
				t.Fatal("accepted unknown parameter")
			}
			for name, limits := range ranges[i] {
				for _, value := range []float64{limits[0], limits[1]} {
					if _, err := factory.create(map[string]float64{name: value}); err != nil {
						t.Fatalf("%s boundary %g: %v", name, value, err)
					}
				}
				for _, value := range []float64{math.Nextafter(limits[0], math.Inf(-1)), math.Nextafter(limits[1], math.Inf(1)), math.NaN(), math.Inf(1), math.Inf(-1)} {
					if _, err := factory.create(map[string]float64{name: value}); err == nil {
						t.Fatalf("accepted %s=%g", name, value)
					}
				}
			}
		})
	}
}

func TestBoundarySettingsProduceFiniteBoundedPCM(t *testing.T) {
	settings := [][]map[string]float64{
		{
			{"frequencyHz": 20, "q": 20, "gainDB": 24},
			{"frequencyHz": 20000, "q": 0.1, "gainDB": -24},
			{"frequencyHz": 20, "q": 0.1, "gainDB": 24},
			{"frequencyHz": 20000, "q": 20, "gainDB": -24},
		},
		{
			{"thresholdDB": -60, "ratio": 20, "attackMs": 0.1, "releaseMs": 10, "makeupDB": 24, "kneeDB": 24},
			{"thresholdDB": 0, "ratio": 1, "attackMs": 200, "releaseMs": 3000, "makeupDB": 24, "kneeDB": 0},
		},
		{
			{"roomSize": 1, "damping": 1, "wet": 1},
			{"roomSize": 1, "damping": 0, "wet": 1},
			{"roomSize": 0, "damping": 0, "wet": 0},
		},
	}
	input := constant(48000, 1, -1)
	input[0], input[1] = math.MaxFloat32, -math.MaxFloat32
	for i, factory := range factories {
		for _, params := range settings[i] {
			node := configured(t, factory.create, params)
			output := render(t, node, input, 4096, false)
			for _, sample := range output {
				if math.IsNaN(float64(sample)) || math.IsInf(float64(sample), 0) || sample < -1 || sample > 1 {
					t.Fatalf("%s produced invalid PCM for boundary settings %v", factory.name, params)
				}
			}
		}
	}
}
