package builtin_test

import (
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/dsp/builtin"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
)

func TestCatalogDefaultsAndAliasesMatchNodeContracts(t *testing.T) {
	registry := audio.NewRegistry()
	if err := builtin.Register(registry); err != nil {
		t.Fatal(err)
	}
	for _, descriptor := range builtin.Catalog() {
		params := make(map[string]float64, len(descriptor.Parameters))
		for _, parameter := range descriptor.Parameters {
			params[parameter.Key] = parameter.DefaultValue
		}
		for _, name := range append([]string{descriptor.Name}, descriptor.Aliases...) {
			node, err := registry.CreateVersioned(audio.NodeSpec{Name: name, Version: descriptor.Version, Params: params})
			if err != nil {
				t.Fatalf("catalog defaults rejected by %s: %v", name, err)
			}
			for _, layout := range descriptor.Input.Layouts {
				input, err := audio.FormatForLayout(layout)
				if err != nil {
					t.Fatal(err)
				}
				output, err := audio.DescribeIO(node, input)
				if err != nil || output.CanContinueDSP != descriptor.Output.CanContinueDSP || descriptor.Output.SameAsInput && output.Format != input {
					t.Fatalf("catalog IO differs for %s: input=%+v output=%+v err=%v", name, input, output, err)
				}
			}
			node.Close()
		}
	}
}

func TestGainAndBypassRejectUnknownParameters(t *testing.T) {
	registry := audio.NewRegistry()
	if err := builtin.Register(registry); err != nil {
		t.Fatal(err)
	}
	for _, spec := range []audio.NodeSpec{
		{Name: "gain", Version: 1, Params: map[string]float64{"gainDB": 0, "unknown": 1}},
		{Name: "gain", Version: 1, Params: map[string]float64{"unknown": 1}},
		{Name: "bypass", Version: 1, Params: map[string]float64{"gainDB": 0}},
	} {
		if node, err := registry.CreateVersioned(spec); err == nil {
			node.Close()
			t.Fatalf("unexpected parameter accepted by %s", spec.Name)
		}
	}
}
