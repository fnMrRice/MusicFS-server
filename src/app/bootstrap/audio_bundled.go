package bootstrap

import (
	"context"
	audioffmpeg "github.com/fnMrRice/MusicFS-server/plugins/decoder/ffmpeg"
	ncmdecoder "github.com/fnMrRice/MusicFS-server/plugins/decoder/ncm"
	"github.com/fnMrRice/MusicFS-server/src/core/config"
)

// 官方实现仅在此处装配，其他实现注册同一工厂契约即可参与运行。
func init() {
	if err := RegisterAudioFactory("musicfs.plugin.decoder.ffmpeg", AudioFactory{Processing: true, Cleanup: audioffmpeg.CleanupCodecOutputs, Create: func(ctx context.Context, c config.Config, p config.Plugin, i config.Instance) (AudioInstance, error) {
		processor, err := audioffmpeg.NewProcessor(ctx, audioffmpeg.Config{Builtin: p.InProcess(), InstanceID: i.InstanceID, FFmpegPath: i.Config.FFmpegPath, FFprobePath: i.Config.FFprobePath, WorkerPath: p.Executable, WorkerArgs: p.Args, WorkerSHA256: pluginChecksum(p), WorkDir: c.DataRoot, MaxConcurrent: c.MaxProcessing, DSPChain: c.AudioDSP.Chain, DSPPlugins: c.AudioDSP.Plugins, OutputLayout: c.AudioChannels.OutputLayout, DisableEffects: !c.AudioDSP.Enabled, DisableChannelMix: !c.AudioChannels.Enabled})
		if err != nil {
			return AudioInstance{}, err
		}
		return AudioInstance{Processor: processor, Probe: processor.ProbeOutputMetadata}, nil
	}}); err != nil {
		panic(err)
	}
	if err := RegisterAudioFactory("musicfs.plugin.decoder.ncm", AudioFactory{Create: func(ctx context.Context, c config.Config, p config.Plugin, i config.Instance) (AudioInstance, error) {
		decoder, err := ncmdecoder.NewProcessor(ncmdecoder.Config{Builtin: p.InProcess(), InstanceID: i.InstanceID, WorkerPath: p.Executable, WorkerArgs: p.Args, WorkerSHA256: pluginChecksum(p), WorkDir: c.DataRoot, MaxConcurrent: c.MaxProcessing})
		if err != nil {
			return AudioInstance{}, err
		}
		return AudioInstance{Decoder: decoder}, nil
	}}); err != nil {
		panic(err)
	}
}
