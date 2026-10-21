package dspchain

import (
	"fmt"
	"maps"
	"math"

	"github.com/fnMrRice/MusicFS-server/plugins/dsp/builtin"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio/dspworker"
)

// Validate 校验配置，不打开文件或启动插件。
func Validate(chain []audio.NodeSpec, plugins []dspworker.Config) error {
	if len(chain) >= audio.MaxChainNodes || len(plugins) > audio.MaxChainNodes {
		return fmt.Errorf("invalid DSP configuration: node limit exceeded")
	}
	external := make(map[string]bool)
	for _, plugin := range plugins {
		if err := plugin.Validate(); err != nil {
			return err
		}
		if external[plugin.Name] {
			return fmt.Errorf("duplicate DSP plugin name")
		}
		external[plugin.Name] = true
	}
	registry := audio.NewRegistry()
	if err := builtin.Register(registry); err != nil {
		return err
	}
	for _, spec := range chain {
		if spec.Version != audio.NodeVersion || len(spec.Params) > 64 {
			return fmt.Errorf("invalid DSP node version or parameters")
		}
		for key, value := range spec.Params {
			if key == "" || len(key) > 64 || math.IsNaN(value) || math.IsInf(value, 0) {
				return fmt.Errorf("invalid DSP node parameter")
			}
		}
		if !external[spec.Name] {
			node, err := registry.CreateVersioned(spec)
			if err != nil {
				return err
			}
			node.Close()
		}
	}
	return nil
}

func Clone(chain []audio.NodeSpec) []audio.NodeSpec {
	if chain == nil {
		return nil
	}
	copy := make([]audio.NodeSpec, len(chain))
	for i, spec := range chain {
		copy[i] = spec
		copy[i].Params = maps.Clone(spec.Params)
	}
	return copy
}
