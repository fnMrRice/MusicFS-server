// Package audioruntime 定义宿主消费的音频能力，与具体解码、编码实现无关。
package audioruntime

import (
	"context"
	"io"

	"github.com/fnMrRice/MusicFS-server/sdk/audioformat"
	"github.com/fnMrRice/MusicFS-server/sdk/audiotypes"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio/dspworker"
	"github.com/fnMrRice/MusicFS-server/src/plugins/dspsettings"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugincontrol"
)

type Decoder interface {
	AudioDecoder() audioformat.Decoder
	AcquireLease(context.Context) (*plugincontrol.Lease, error)
	SetGate(*plugincontrol.Gate)
	PluginVersion() string
}

type Capabilities struct {
	Version, DecoderSHA256 string
	Normalization          bool
	Profiles               []audiotypes.Profile
}

// Processor 的参数视图必须保持原实例的生命周期，实例由装配注册表注入。
type Processor interface {
	Decoder
	Available() bool
	RuntimeCapabilities() Capabilities
	Process(context.Context, audiotypes.Plan, io.ReadSeeker, int64, io.Writer) error
	Normalize(context.Context, audio.NormalizeRequest, io.ReaderAt, int64, io.Writer) (audio.PCMInfo, error)
	ProbeOutputMetadata(context.Context, io.ReadSeeker, int64) (audiotypes.Metadata, error)
	OutputProfile() (audiotypes.Profile, error)
	LookupProfile(string) (audiotypes.Profile, bool)
	SupportsProfile(audiotypes.Profile) bool
	SupportsDecodeOutput(string) error
	SelectSettings(context.Context, dspsettings.Options) (Processor, error)
	SelectSourceBitrate(int64) (Processor, error)
	SelectSourceChannels(int, bool) Processor
	SelectPolicy(bool, bool) Processor
	EffectsDisabled() bool
	ChannelMixDisabled() bool
	RequiresChannelConversion() bool
	HasDSP() bool
	DSPChain() []audio.NodeSpec
	DSPPlugins() []dspworker.Config
	DSPPluginVersion(string) string
	Defaults() dspsettings.Options
	AvailableOutputLayouts() []string
	ValidateSettings(context.Context, dspsettings.Options) error
	RuntimeIdentity(string) audiotypes.ProcessingIdentity
	DeploymentIdentityValue() audiotypes.ProcessingIdentity
}
