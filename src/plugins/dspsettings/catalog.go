package dspsettings

import (
	"math"
	"sort"

	"github.com/fnMrRice/MusicFS-server/plugins/dsp/builtin"
)

func catalog(cfg Config) []builtin.Descriptor {
	if cfg.DisableEffects {
		return []builtin.Descriptor{}
	}
	items := builtin.Catalog()
	known := map[string]bool{}
	for _, item := range items {
		known[item.Name] = true
		for _, alias := range item.Aliases {
			known[alias] = true
		}
	}
	for _, plugin := range cfg.Plugins {
		if plugin.Name == "musicfs.dsp.gain" && plugin.Builtin {
			items[0].Aliases = append(items[0].Aliases, plugin.Name)
			continue
		}
		if known[plugin.Name] {
			continue
		}
		parameters := map[string]float64{}
		for _, node := range cfg.Defaults.Chain {
			if node.Name == plugin.Name {
				for key, value := range node.Params {
					parameters[key] = value
				}
			}
		}
		keys := make([]string, 0, len(parameters))
		for key := range parameters {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		fields := []builtin.Parameter{}
		for _, key := range keys {
			fields = append(fields, builtin.Parameter{Key: key, Label: key, DefaultValue: parameters[key], Min: -math.MaxFloat64, Max: math.MaxFloat64, Step: 0.001})
		}
		items = append(items, builtin.Descriptor{Name: plugin.Name, Version: 1, DisplayName: plugin.Name,
			Description: "已由管理员配置的外部 DSP，仅支持双声道和不超过 4096 帧的缓冲区。",
			Input:       builtin.Port{SampleFormat: "f32le", SampleRate: 48000, Layouts: []string{"stereo"}},
			Output:      builtin.OutputPort{CanContinueDSP: true, SameAsInput: true, Layouts: []string{"stereo"}}, Parameters: fields})
	}
	return items
}
