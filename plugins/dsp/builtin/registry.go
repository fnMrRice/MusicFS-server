// Package builtin 装配官方 DSP；有状态效果直接在 Server 主进程执行。
package builtin

import (
	"strings"

	"github.com/fnMrRice/MusicFS-server/plugins/dsp/channelmix"
	"github.com/fnMrRice/MusicFS-server/plugins/dsp/effects"
	dsp "github.com/fnMrRice/MusicFS-server/plugins/dsp/gain"
	"github.com/fnMrRice/MusicFS-server/sdk/bundledplugin"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio/dspworker"
)

// EffectName 将官方效果别名映射为短名；第三方名称不会被接管。
func EffectName(name string) string {
	if name == channelmix.PluginID {
		return "channelmix"
	}
	short := strings.TrimPrefix(name, "musicfs.dsp.")
	switch short {
	case "equalizer", "compressor", "reverb", "channelmix":
		return short
	default:
		return ""
	}
}

func registerOfficial(registry *audio.Registry) error {
	if err := dsp.Register(registry); err != nil {
		return err
	}
	if err := effects.Register(registry); err != nil {
		return err
	}
	if err := channelmix.Register(registry); err != nil {
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

func init() {
	registry := audio.NewRegistry()
	if err := registerOfficial(registry); err != nil {
		panic(err)
	}
	for _, descriptor := range Catalog() {
		descriptor.PluginID = "musicfs.dsp." + descriptor.Name
		descriptor.PluginVersion = Version(descriptor.Name)
		if descriptor.Name == "gain" {
			descriptor.Capability = "gain"
			descriptor.PluginVersion = bundledplugin.GainVersion
			descriptor.Aliases = append(descriptor.Aliases, "musicfs.dsp.gain")
		}
		if descriptor.Name == "channelmix" {
			descriptor.PluginID = channelmix.PluginID
			descriptor.Capability = "channelConversion"
		}
		factory, ok := registry.Factory(descriptor.Name)
		if !ok {
			panic("missing official DSP factory")
		}
		if err := audio.RegisterDefinition(descriptor, factory); err != nil {
			panic(err)
		}
	}
	factory, _ := registry.Factory("gain")
	if err := dspworker.RegisterBuiltin("musicfs.dsp.gain", bundledplugin.GainVersion, factory); err != nil {
		panic(err)
	}
}

// Register 组装当前已注册节点，第三方节点遵循相同声明与工厂边界。
func Register(registry *audio.Registry) error { return audio.RegisterDefinitions(registry) }

// Version 来自随构建交付的实现声明，且不依赖插件子进程握手。
func Version(name string) string {
	if short := EffectName(name); short != "" {
		if short == "channelmix" {
			return bundledplugin.ChannelMixVersion
		}
		return bundledplugin.Version("musicfs.dsp." + short)
	}
	return ""
}
