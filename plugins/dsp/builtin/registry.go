// Package builtin 装配官方 DSP；有状态效果直接在 Server 主进程执行。
package builtin

import (
	"strings"

	"github.com/fnMrRice/MusicFS-server/plugins/dsp/effects"
	dsp "github.com/fnMrRice/MusicFS-server/plugins/dsp/gain"
	"github.com/fnMrRice/MusicFS-server/sdk/bundledplugin"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
)

// EffectName 将官方效果别名映射为短名；第三方名称不会被接管。
func EffectName(name string) string {
	short := strings.TrimPrefix(name, "musicfs.dsp.")
	switch short {
	case "equalizer", "compressor", "reverb":
		return short
	default:
		return ""
	}
}

func Register(registry *audio.Registry) error {
	if err := dsp.Register(registry); err != nil {
		return err
	}
	if err := effects.Register(registry); err != nil {
		return err
	}
	for name, factory := range map[string]audio.Factory{
		"musicfs.dsp.equalizer":  effects.NewEqualizer,
		"musicfs.dsp.compressor": effects.NewCompressor,
		"musicfs.dsp.reverb":     effects.NewReverb,
	} {
		if err := registry.Register(name, factory); err != nil {
			return err
		}
	}
	return nil
}

// Version 来自随构建交付的实现声明，且不依赖插件子进程握手。
func Version(name string) string {
	if short := EffectName(name); short != "" {
		return bundledplugin.Version("musicfs.dsp." + short)
	}
	return ""
}
