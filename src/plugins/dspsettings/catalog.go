package dspsettings

import (
	"math"
	"sort"

	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
)

func catalog(cfg Config) []audio.Descriptor {
	if cfg.DisableEffects {
		return []audio.Descriptor{}
	}
	items := audio.Catalog()
	if cfg.DisableChannelMix {
		filtered := items[:0]
		for _, item := range items {
			if item.Capability != "channelConversion" {
				filtered = append(filtered, item)
			}
		}
		items = filtered
	}
	known := map[string]bool{}
	for _, item := range items {
		known[item.Name] = true
		for _, alias := range item.Aliases {
			known[alias] = true
		}
	}
	for _, plugin := range cfg.Plugins {
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
		fields := []audio.Parameter{}
		for _, key := range keys {
			fields = append(fields, audio.Parameter{Key: key, Label: key, DefaultValue: parameters[key], Min: -math.MaxFloat64, Max: math.MaxFloat64, Step: 0.001})
		}
		items = append(items, audio.Descriptor{Name: plugin.Name, Version: 1, DisplayName: plugin.Name,
			Description: "已由管理员配置的协议 DSP，仅支持双声道和不超过 4096 帧的缓冲区。",
			Input:       audio.Port{SampleFormat: "f32le", SampleRate: 48000, Layouts: []string{"stereo"}},
			Output:      audio.OutputPort{CanContinueDSP: true, SameAsInput: true, Layouts: []string{"stereo"}}, Parameters: fields})
	}
	return items
}

// WithoutChannelMixNodes 根据声明能力过滤声道转换节点，不绑定插件名称。
func WithoutChannelMixNodes(chain []audio.NodeSpec) []audio.NodeSpec {
	result := []audio.NodeSpec{}
	for _, node := range chain {
		if !audio.HasCapability(node.Name, "channelConversion") {
			result = append(result, node)
		}
	}
	return result
}
