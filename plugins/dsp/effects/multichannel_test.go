package effects_test

import (
	"math"
	"slices"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/dsp/effects"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
)

func configuredFormat(t *testing.T, factory audio.Factory, params map[string]float64, format audio.Format) audio.Node {
	t.Helper()
	node, err := factory(params)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = node.Close() })
	if err := node.Configure(format); err != nil {
		t.Fatal(err)
	}
	return node
}

func renderFormat(t *testing.T, node audio.Node, format audio.Format, input []float32, chunk int, discontinuity bool) []float32 {
	t.Helper()
	result := slices.Clone(input)
	for start := 0; start < len(result); start += chunk * format.Channels {
		end := min(start+chunk*format.Channels, len(result))
		block := audio.Block{Format: format, Samples: result[start:end], Frames: (end - start) / format.Channels,
			Sequence: uint64(start / format.Channels), PositionFrames: int64(start / format.Channels),
			End: end == len(result), Discontinuity: discontinuity && start == 0}
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

func multichannelSignal(frames, channels int) []float32 {
	samples := make([]float32, frames*channels)
	for frame := range frames {
		for channel := range channels {
			samples[frame*channels+channel] = float32(0.03 * float64(channel+1) * math.Sin(float64(frame)*(0.137+0.023*float64(channel))))
		}
	}
	return samples
}

func TestMultichannelBlockSizesAndStateLifecycle(t *testing.T) {
	for _, factory := range factories {
		t.Run(factory.name, func(t *testing.T) {
			reused, err := factory.create(factory.params)
			if err != nil {
				t.Fatal(err)
			}
			defer reused.Close()
			for _, layout := range []string{"mono", "stereo", "5.1", "7.1", "mono"} {
				format, _ := audio.FormatForLayout(layout)
				input := multichannelSignal(7000, format.Channels)
				baseline := renderFormat(t, configuredFormat(t, factory.create, factory.params, format), format, input, 4096, false)
				if err := reused.Configure(format); err != nil {
					t.Fatal(err)
				}
				if actual := renderFormat(t, reused, format, input, 257, false); !slices.Equal(actual, baseline) {
					t.Fatalf("configure or block-size mismatch for %s", layout)
				}
				if err := reused.Reset(); err != nil {
					t.Fatal(err)
				}
				if actual := renderFormat(t, reused, format, input, 1, false); !slices.Equal(actual, baseline) {
					t.Fatalf("reset or one-frame block mismatch for %s", layout)
				}
				if actual := renderFormat(t, reused, format, input, 1023, true); !slices.Equal(actual, baseline) {
					t.Fatalf("discontinuity failed for %s", layout)
				}
				wrongFormat := audio.Canonical
				if format == audio.Canonical {
					wrongFormat, _ = audio.FormatForLayout("mono")
				}
				wrong := audio.Block{Format: wrongFormat, Frames: 1, Samples: make([]float32, wrongFormat.Channels)}
				if err := reused.Process(&wrong); err == nil {
					t.Fatal("accepted unconfigured layout change")
				}
			}
		})
	}
}

func TestMultichannelEffectsDoNotLeakAcrossChannelsOrInstances(t *testing.T) {
	for _, layout := range []string{"mono", "5.1", "7.1"} {
		format, _ := audio.FormatForLayout(layout)
		for _, factory := range factories {
			for active := range format.Channels {
				input := make([]float32, 4000*format.Channels)
				input[active] = 0.5
				node := configuredFormat(t, factory.create, factory.params, format)
				output := renderFormat(t, node, format, input, 4096, false)
				for i, sample := range output {
					if i%format.Channels != active && sample != 0 {
						t.Fatalf("%s leaked channel %d in %s", factory.name, active, layout)
					}
				}
			}
			first := configuredFormat(t, factory.create, factory.params, format)
			second := configuredFormat(t, factory.create, factory.params, format)
			input := multichannelSignal(5000, format.Channels)
			prefix := renderFormat(t, first, format, input[:format.Channels*1700], 257, false)
			baseline := renderFormat(t, second, format, input, 4096, false)
			combined := append(prefix, renderFormat(t, first, format, input[format.Channels*1700:], 257, false)...)
			if !slices.Equal(combined, baseline) {
				t.Fatalf("%s shares state across %s instances", factory.name, layout)
			}
		}
	}
}

func TestMultichannelCompressorLinksSurroundPeaks(t *testing.T) {
	for _, layout := range []string{"5.1", "7.1"} {
		format, _ := audio.FormatForLayout(layout)
		node := configuredFormat(t, effects.NewCompressor, map[string]float64{"thresholdDB": -20, "ratio": 4, "attackMs": 0.1, "kneeDB": 0}, format)
		input := make([]float32, 2400*format.Channels)
		for i := range input {
			input[i] = 0.1
			if i%format.Channels == format.Channels-1 {
				input[i] = -0.8
			}
		}
		output := renderFormat(t, node, format, input, 4096, false)
		gain := math.Pow(10, -(20*math.Log10(0.8)+20)*0.75/20)
		for channel := range format.Channels {
			i := len(output) - format.Channels + channel
			if math.Abs(float64(output[i])-float64(input[i])*gain) > 1e-6 {
				t.Fatalf("unlinked %s channel %d", layout, channel)
			}
		}
	}
}

func TestSurroundReverbPreservesLFE(t *testing.T) {
	for _, layout := range []string{"5.1", "7.1"} {
		format, _ := audio.FormatForLayout(layout)
		for _, wet := range []float64{0, 0.5, 1} {
			node := configuredFormat(t, effects.NewReverb, map[string]float64{"wet": wet}, format)
			input := multichannelSignal(7000, format.Channels)
			output := renderFormat(t, node, format, input, 257, false)
			for frame := range 7000 {
				i := frame*format.Channels + 3
				if output[i] != input[i] {
					t.Fatalf("LFE changed for %s wet=%g", layout, wet)
				}
			}
		}
	}
}
