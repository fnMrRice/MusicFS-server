package dspworker_test

import (
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio/dspworker"
	"github.com/fnMrRice/MusicFS-server/src/plugins/dspsettings"
)

// 该节点直接交换左右声道，不复用任何官方 DSP 算法。
type independentSwap struct{}

func (independentSwap) DescribeIO(format audio.Format) (audio.Output, error) {
	return audio.PCMOutput(format)
}
func (n independentSwap) ConfigureIO(format audio.Format) (audio.Output, error) {
	return n.DescribeIO(format)
}
func (independentSwap) Process(block *audio.Block) error {
	for i := 0; i+1 < len(block.Samples); i += 2 {
		block.Samples[i], block.Samples[i+1] = block.Samples[i+1], block.Samples[i]
	}
	return nil
}
func (independentSwap) Reset() error       { return nil }
func (independentSwap) Drain() error       { return nil }
func (independentSwap) Close() error       { return nil }
func (independentSwap) DelayFrames() int64 { return 0 }
func (independentSwap) Stateless() bool    { return true }

func TestIndependentBuiltinDSPExecutesRegisteredFactory(t *testing.T) {
	const name = "test.independent.swap"
	factory := func(map[string]float64) (audio.Node, error) { return independentSwap{}, nil }
	if err := dspworker.RegisterBuiltin(name, "2.3.4", factory); err != nil {
		t.Fatal(err)
	}
	options := dspsettings.Options{Chain: []audio.NodeSpec{{Name: name, Version: audio.NodeVersion}}, OutputLayout: "stereo", BufferFrames: 64}
	settings := dspsettings.New(nil, func() dspsettings.Config {
		return dspsettings.Config{Available: true, OutputLayouts: []string{"stereo"}, Plugins: []dspworker.Config{{Name: name, Builtin: true}}}
	})
	if _, err := settings.Validate(t.Context(), options); err != nil {
		t.Fatal("registered protocol DSP was rejected by settings", err)
	}
	process, err := dspworker.New(t.Context(), dspworker.Config{Name: name, Builtin: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer process.Close()
	block := audio.Block{Format: audio.Canonical, Samples: []float32{1, -1, .25, -.5}, Frames: 2, Sequence: 5, PositionFrames: 100}
	if err = process.Process(&block); err != nil {
		t.Fatal(err)
	}
	if process.PluginVersion() != "2.3.4" || block.Samples[0] != -1 || block.Samples[1] != 1 || block.Samples[2] != -.5 || block.Samples[3] != .25 || block.Sequence != 5 || block.PositionFrames != 100 {
		t.Fatalf("registered DSP factory did not process the real block: %+v", block)
	}
	if err = audio.RegisterDefinition(audio.Descriptor{Name: name, Version: audio.NodeVersion, PluginID: "test.dsp.swap", PluginVersion: "2.3.4", DisplayName: "Swap", Input: audio.Port{SampleFormat: "f32le", SampleRate: 48000, Layouts: []string{"stereo"}}, Output: audio.OutputPort{CanContinueDSP: true, SameAsInput: true, Layouts: []string{"stereo"}}}, factory); err != nil {
		t.Fatal(err)
	}
	registry := audio.NewRegistry()
	if err = audio.RegisterDefinitions(registry); err != nil {
		t.Fatal(err)
	}
	node, err := registry.CreateVersioned(options.Chain[0])
	if err != nil {
		t.Fatal(err)
	}
	defer node.Close()
	if _, err = node.ConfigureIO(audio.Canonical); err != nil {
		t.Fatal(err)
	}
	if err = node.Process(&block); err != nil || block.Samples[0] != 1 || block.Samples[1] != -1 {
		t.Fatal("registered native factory was not selected", err)
	}
	if _, err = settings.Validate(t.Context(), options); err != nil {
		t.Fatal("registered native DSP was rejected by settings", err)
	}
}
