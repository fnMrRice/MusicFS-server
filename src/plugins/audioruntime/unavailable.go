package audioruntime

import (
	"context"
	"fmt"
	"github.com/fnMrRice/MusicFS-server/sdk/audioformat"
	"github.com/fnMrRice/MusicFS-server/sdk/audiotypes"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio/dspworker"
	"github.com/fnMrRice/MusicFS-server/src/plugins/dspsettings"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugincontrol"
	"io"
)

// Unavailable 为无处理实例的原样播放提供一致默认值，也供能力有限的实现嵌入。
type Unavailable struct{}

func unavailable() error { return fmt.Errorf("UNSUPPORTED: audio processor is unavailable") }
func Safe(p Processor) Processor {
	if p == nil || !p.Available() {
		return Unavailable{}
	}
	return p
}
func (Unavailable) Available() bool                                            { return false }
func (Unavailable) RuntimeCapabilities() Capabilities                          { return Capabilities{} }
func (Unavailable) AudioDecoder() audioformat.Decoder                          { return audioformat.Decoder{} }
func (Unavailable) AcquireLease(context.Context) (*plugincontrol.Lease, error) { return nil, nil }
func (Unavailable) SetGate(*plugincontrol.Gate)                                {}
func (Unavailable) PluginVersion() string                                      { return "" }
func (Unavailable) Process(context.Context, audiotypes.Plan, io.ReadSeeker, int64, io.Writer) error {
	return unavailable()
}
func (Unavailable) Normalize(context.Context, audio.NormalizeRequest, io.ReaderAt, int64, io.Writer) (audio.PCMInfo, error) {
	return audio.PCMInfo{}, unavailable()
}
func (Unavailable) ProbeOutputMetadata(context.Context, io.ReadSeeker, int64) (audiotypes.Metadata, error) {
	return audiotypes.Metadata{}, unavailable()
}
func (Unavailable) OutputProfile() (audiotypes.Profile, error) {
	return audiotypes.Profile{}, unavailable()
}
func (Unavailable) LookupProfile(string) (audiotypes.Profile, bool) {
	return audiotypes.Profile{}, false
}
func (Unavailable) SupportsProfile(audiotypes.Profile) bool { return false }
func (Unavailable) SupportsDecodeOutput(string) error       { return unavailable() }
func (u Unavailable) SelectSettings(context.Context, dspsettings.Options) (Processor, error) {
	return u, unavailable()
}
func (u Unavailable) SelectSourceBitrate(int64) (Processor, error) { return u, unavailable() }
func (u Unavailable) SelectSourceChannels(int, bool) Processor     { return u }
func (u Unavailable) SelectPolicy(bool, bool) Processor            { return u }
func (Unavailable) EffectsDisabled() bool                          { return false }
func (Unavailable) ChannelMixDisabled() bool                       { return false }
func (Unavailable) RequiresChannelConversion() bool                { return false }
func (Unavailable) HasDSP() bool                                   { return false }
func (Unavailable) DSPChain() []audio.NodeSpec                     { return nil }
func (Unavailable) DSPPlugins() []dspworker.Config                 { return nil }
func (Unavailable) DSPPluginVersion(string) string                 { return "" }
func (Unavailable) Defaults() dspsettings.Options {
	return dspsettings.Options{Chain: []audio.NodeSpec{}, OutputLayout: "stereo", BufferFrames: 4096}
}
func (Unavailable) AvailableOutputLayouts() []string                            { return nil }
func (Unavailable) ValidateSettings(context.Context, dspsettings.Options) error { return unavailable() }
func (Unavailable) RuntimeIdentity(string) audiotypes.ProcessingIdentity {
	return audiotypes.ProcessingIdentity{}
}
func (Unavailable) DeploymentIdentityValue() audiotypes.ProcessingIdentity {
	return audiotypes.ProcessingIdentity{}
}
