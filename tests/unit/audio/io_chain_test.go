package audio_test

import (
	"math"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/dsp/builtin"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
)

type legacyNode struct {
	configured bool
	format     audio.Format
	process    func(*audio.Block)
}

func (n *legacyNode) Configure(format audio.Format) error {
	n.configured, n.format = true, format
	return nil
}
func (n *legacyNode) Process(block *audio.Block) error {
	if n.process != nil {
		n.process(block)
	}
	return nil
}
func (*legacyNode) Reset() error       { return nil }
func (*legacyNode) Drain() error       { return nil }
func (*legacyNode) Close() error       { return nil }
func (*legacyNode) DelayFrames() int64 { return 0 }

type explicitNode struct {
	legacyNode
	output audio.Output
}

func (n *explicitNode) DescribeIO(audio.Format) (audio.Output, error) { return n.output, nil }
func (n *explicitNode) ConfigureIO(format audio.Format) (audio.Output, error) {
	_ = n.Configure(format)
	return n.output, nil
}

func registryFor(t *testing.T, name string, node audio.Node) *audio.Registry {
	t.Helper()
	registry := audio.NewRegistry()
	if err := registry.Register(name, func(map[string]float64) (audio.Node, error) { return node, nil }); err != nil {
		t.Fatal(err)
	}
	return registry
}

func TestLegacyNodeIOPreservesFormatWithoutConfiguring(t *testing.T) {
	node := &legacyNode{}
	want := audio.Output{Format: audio.Canonical, CanContinueDSP: true}
	got, err := audio.DescribeIO(node, audio.Canonical)
	if err != nil || got != want || node.configured {
		t.Fatalf("declaration changed node state: output=%+v err=%v configured=%v", got, err, node.configured)
	}
	chain, err := audio.NewChain(registryFor(t, "legacy", node), []audio.NodeSpec{{Name: "legacy", Version: 1}})
	if err != nil {
		t.Fatal(err)
	}
	defer chain.Close()
	got, err = chain.ConfigureIO(audio.Canonical)
	if err != nil || got != want || chain.Output() != want || !node.configured {
		t.Fatalf("legacy IO configuration failed: output=%+v err=%v", got, err)
	}
	if err = chain.Process(&audio.Block{Format: audio.Canonical, Frames: 1, Samples: []float32{0.2, -0.3}}); err != nil {
		t.Fatal(err)
	}
}

func TestTerminalDSPOutputMustBeLast(t *testing.T) {
	for _, terminalFirst := range []bool{true, false} {
		node := &explicitNode{output: audio.Output{Format: audio.Canonical, CanContinueDSP: false}}
		registry := registryFor(t, "terminal", node)
		if err := registry.Register("legacy", func(map[string]float64) (audio.Node, error) { return &legacyNode{}, nil }); err != nil {
			t.Fatal(err)
		}
		specs := []audio.NodeSpec{{Name: "legacy", Version: 1}, {Name: "terminal", Version: 1}}
		if terminalFirst {
			specs[0], specs[1] = specs[1], specs[0]
		}
		declared, declarationErr := audio.NegotiateChain(registry, specs, audio.Canonical)
		if node.configured || (declarationErr != nil) != terminalFirst {
			t.Fatalf("terminal declaration mismatch: output=%+v err=%v configured=%v", declared, declarationErr, node.configured)
		}
		chain, err := audio.NewChain(registry, specs)
		if err != nil {
			t.Fatal(err)
		}
		configured, err := chain.ConfigureIO(audio.Canonical)
		if (err != nil) != terminalFirst {
			t.Fatalf("terminal configuration mismatch: output=%+v err=%v", configured, err)
		}
		if !terminalFirst && (declared.CanContinueDSP || configured != declared) {
			t.Fatalf("terminal flag lost: declared=%+v configured=%+v", declared, configured)
		}
		if terminalFirst && chain.Process(&audio.Block{Format: audio.Canonical}) == nil {
			t.Fatal("failed chain configuration remained usable")
		}
		chain.Close()
	}
}

func TestChainRejectsOutputDeclarationAndTimingViolations(t *testing.T) {
	mono, _ := audio.FormatForLayout("mono")
	for name, corrupt := range map[string]func(*audio.Block){
		"format": func(block *audio.Block) {
			block.Format, block.Samples = mono, []float32{0.5}
		},
		"frames":        func(block *audio.Block) { block.Frames, block.Samples = 0, nil },
		"sequence":      func(block *audio.Block) { block.Sequence++ },
		"position":      func(block *audio.Block) { block.PositionFrames++ },
		"end":           func(block *audio.Block) { block.End = !block.End },
		"discontinuity": func(block *audio.Block) { block.Discontinuity = !block.Discontinuity },
		"nonfinite":     func(block *audio.Block) { block.Samples[0] = float32(math.NaN()) },
	} {
		t.Run(name, func(t *testing.T) {
			node := &explicitNode{legacyNode: legacyNode{process: corrupt}, output: audio.Output{Format: audio.Canonical, CanContinueDSP: true}}
			chain, err := audio.NewChain(registryFor(t, "bad", node), []audio.NodeSpec{{Name: "bad", Version: 1}})
			if err != nil {
				t.Fatal(err)
			}
			defer chain.Close()
			if err := chain.Configure(audio.Canonical); err != nil {
				t.Fatal(err)
			}
			block := &audio.Block{Format: audio.Canonical, Frames: 1, Samples: []float32{0.2, -0.3}, PositionFrames: 256, Sequence: 7, End: true}
			if err := chain.Process(block); err == nil {
				t.Fatal("invalid DSP output accepted")
			}
		})
	}
}

func TestBuiltinChainNegotiatesEachNodeAndPreservesFrameTimeline(t *testing.T) {
	registry := audio.NewRegistry()
	if err := builtin.Register(registry); err != nil {
		t.Fatal(err)
	}
	specs := []audio.NodeSpec{
		{Name: "channelmix", Version: 1, Params: map[string]float64{"outputChannels": 6}},
		{Name: "gain", Version: 1, Params: map[string]float64{"gainDB": -6}},
		{Name: "equalizer", Version: 1},
		{Name: "musicfs.dsp.channelmix", Version: 1, Params: map[string]float64{"outputChannels": 1}},
		{Name: "compressor", Version: 1},
		{Name: "musicfs.plugin.decoder.channelmix", Version: 1, Params: map[string]float64{"outputChannels": 8}},
		{Name: "reverb", Version: 1},
	}
	declared, err := audio.NegotiateChain(registry, specs, audio.Canonical)
	if err != nil || declared.Format.Layout != "7.1" || !declared.CanContinueDSP {
		t.Fatalf("chain IO mismatch: output=%+v err=%v", declared, err)
	}
	chain, err := audio.NewChain(registry, specs)
	if err != nil {
		t.Fatal(err)
	}
	defer chain.Close()
	if err = chain.Configure(audio.Canonical); err != nil {
		t.Fatal(err)
	}
	if chain.Output() != declared || chain.Stateless() {
		t.Fatal("configured chain differs from declaration")
	}
	block := &audio.Block{Format: audio.Canonical, Frames: audio.MaxBlockFrames, Samples: make([]float32, audio.MaxBlockFrames*2), Sequence: 9, PositionFrames: 128, End: true, Discontinuity: true}
	block.Samples[0], block.Samples[1] = 0.5, -0.25
	if err = chain.Process(block); err != nil {
		t.Fatal(err)
	}
	if block.Format != declared.Format || len(block.Samples) != audio.MaxBlockFrames*8 || block.Frames != audio.MaxBlockFrames || block.Sequence != 9 || block.PositionFrames != 128 || !block.End || !block.Discontinuity {
		t.Fatalf("conversion lost timeline metadata: %+v", *block)
	}
}

func TestEmptyChainAndFailedReconfiguration(t *testing.T) {
	chain, err := audio.NewChain(audio.NewRegistry(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = chain.Process(nil); err == nil {
		t.Fatal("unconfigured chain accepted block")
	}
	if err = chain.Configure(audio.Canonical); err != nil {
		t.Fatal(err)
	}
	if err = chain.Process(nil); err == nil {
		t.Fatal("nil block accepted")
	}
	mono, _ := audio.FormatForLayout("mono")
	if err = chain.Process(&audio.Block{Format: mono}); err == nil {
		t.Fatal("different input format accepted")
	}
	if err = chain.Configure(audio.Format{}); err == nil {
		t.Fatal("invalid format accepted")
	}
	if chain.Output() != (audio.Output{}) || chain.Process(&audio.Block{Format: audio.Canonical}) == nil {
		t.Fatal("failed configuration retained previous state")
	}
}
