package audioffmpeg

import (
	"fmt"
	"io"

	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
)

// ChannelMixDisabled 表示禁用声道插件；必要的编码格式适配由 FFmpeg 完成。
func (p *Processor) ChannelMixDisabled() bool { return p != nil && p.config.DisableChannelMix }

// WithoutChannelMixNodes 去掉历史设置中的所有内置声道插件别名。
func WithoutChannelMixNodes(chain []audio.NodeSpec) []audio.NodeSpec {
	result := []audio.NodeSpec{}
	for _, node := range chain {
		switch node.Name {
		case "channelmix", "musicfs.dsp.channelmix", "musicfs.plugin.decoder.channelmix":
		default:
			result = append(result, node)
		}
	}
	return result
}

// WithoutChannelMix 冻结无声道插件视图，旧输出布局不再进入缓存身份。
func (p *Processor) WithoutChannelMix() *Processor {
	if p == nil || p.config.DisableChannelMix {
		return p
	}
	config := p.config
	config.DisableChannelMix, config.OutputLayout = true, "stereo"
	config.DSPChain = WithoutChannelMixNodes(config.DSPChain)
	identity := p.identity
	identity.PipelineRevision = dspRevision(config, identity.WorkerSHA256)
	return &Processor{parent: p.runtime(), config: config, Capabilities: p.Capabilities, dspVersions: p.dspVersions,
		slots: p.slots, normalizeSlot: p.normalizeSlot, identity: identity}
}

// WithSourceChannels 优先保留来源声道；不支持环绕声编码时退回立体声兼容输出。
func (p *Processor) WithSourceChannels(channels int, allowSurround bool) *Processor {
	if p == nil || !p.config.DisableChannelMix {
		return p
	}
	id := "mp3-128"
	if channels == 1 {
		id = "mp3-mono-128"
	} else if allowSurround {
		switch channels {
		case 6:
			id = "opus-5.1"
		case 8:
			id = "opus-7.1"
		}
	}
	profile, _ := ProfileByID(id)
	if !p.SupportsProfile(profile) {
		id = "mp3-128"
	}
	return &Processor{parent: p.runtime(), config: p.config, Capabilities: p.Capabilities, dspVersions: p.dspVersions,
		slots: p.slots, normalizeSlot: p.normalizeSlot, identity: p.identity, selectedProfile: id}
}

// SupportsProfile 校验编码器能力；MP3 的单声道与立体声共享同一编码器。
func (p *Processor) SupportsProfile(profile Profile) bool {
	if p == nil {
		return false
	}
	for _, supported := range p.Capabilities.Profiles {
		if profile.Codec == supported.Codec && (profile.Layout == supported.Layout || profile.Codec == "mp3" && profile.Layout == "mono") {
			return true
		}
	}
	return false
}

// decodedWAVWithoutMix 只读 FFmpeg 已适配的 PCM，不创建声道插件或再混音。
func decodedWAVWithoutMix(input io.Reader, expected audio.Format, output string) (io.Reader, error) {
	actual, pcm, err := readDecodedWAVOutput(input, output)
	if err != nil {
		return nil, err
	}
	if actual != expected {
		return nil, fmt.Errorf("UNSUPPORTED: decoder output differs from selected encoding layout")
	}
	return pcm, nil
}
