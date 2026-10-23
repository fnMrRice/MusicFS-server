package audioffmpeg

import (
	"context"
	"github.com/fnMrRice/MusicFS-server/sdk/audiotypes"
	"github.com/fnMrRice/MusicFS-server/src/plugins/audiocodecs"
	"github.com/fnMrRice/MusicFS-server/src/plugins/audioruntime"
	"github.com/fnMrRice/MusicFS-server/src/plugins/codecsettings"
	"github.com/fnMrRice/MusicFS-server/src/plugins/dspsettings"
	"github.com/fnMrRice/MusicFS-server/src/plugins/settings"
)

var _ audioruntime.Processor = (*Processor)(nil)

func init() {
	if err := codecsettings.Register(settings.Provider{ID: "musicfs.plugin.decoder.ffmpeg", Schema: SettingsSchema()}, "decodeOutput"); err != nil {
		panic(err)
	}
	if err := audioruntime.RegisterProfiles("musicfs.plugin.decoder.ffmpeg", ProfileByID); err != nil {
		panic(err)
	}
	inspectors := map[string]func(string, int64) (audiotypes.Metadata, error){
		"audio/wav": func(path string, size int64) (audiotypes.Metadata, error) {
			v, err := InspectNativeWave(path, size)
			rate := int64(v.SampleRate)
			return audiotypes.Metadata{Codec: &v.Codec, Container: &v.Container, DurationMs: &v.DurationMs, BitrateBps: &v.BitrateBps, Channels: v.Channels, SampleRateHz: &rate}, err
		},
		"audio/flac": func(path string, size int64) (audiotypes.Metadata, error) {
			v, err := InspectNativeFLAC(path, size)
			rate := int64(v.SampleRate)
			return audiotypes.Metadata{DurationMs: v.DurationMs, Channels: v.Channels, SampleRateHz: &rate}, err
		},
	}
	if err := audiocodecs.Register((*Processor)(nil).AudioDecoder(), inspectors); err != nil {
		panic(err)
	}
}

func (p *Processor) Available() bool { return p != nil }
func (p *Processor) RuntimeCapabilities() audioruntime.Capabilities {
	if p == nil {
		return audioruntime.Capabilities{}
	}
	return audioruntime.Capabilities{Version: p.Capabilities.Version, DecoderSHA256: p.Capabilities.FFmpegSHA256, Normalization: p.Capabilities.Normalization, Profiles: append([]Profile(nil), p.Capabilities.Profiles...)}
}
func (p *Processor) LookupProfile(id string) (Profile, bool) { return ProfileByID(id) }
func (p *Processor) SelectSettings(ctx context.Context, options dspsettings.Options) (audioruntime.Processor, error) {
	return p.WithSettings(ctx, options)
}
func (p *Processor) SelectSourceBitrate(value int64) (audioruntime.Processor, error) {
	return p.WithSourceBitrate(value)
}
func (p *Processor) SelectSourceChannels(value int, surround bool) audioruntime.Processor {
	return p.WithSourceChannels(value, surround)
}
func (p *Processor) SelectPolicy(effects, channels bool) audioruntime.Processor {
	if effects {
		p = p.WithoutEffects()
	}
	if channels {
		p = p.WithoutChannelMix()
	}
	return p
}
func runtimeIdentity(i ProcessingIdentity) audiotypes.ProcessingIdentity {
	return audiotypes.ProcessingIdentity{WorkerSHA256: i.WorkerSHA256, DecoderSHA256: i.FFmpegSHA256, PluginVersion: i.PluginVersion, PipelineRevision: i.PipelineRevision}
}
func (p *Processor) RuntimeIdentity(output string) audiotypes.ProcessingIdentity {
	return runtimeIdentity(p.ProcessingIdentityForDecodeOutput(output))
}
func (p *Processor) DeploymentIdentityValue() audiotypes.ProcessingIdentity {
	return runtimeIdentity(p.DeploymentIdentity())
}
