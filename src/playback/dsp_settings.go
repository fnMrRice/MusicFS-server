package playback

import (
	"context"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/playback/timeline"
	"github.com/fnMrRice/MusicFS-server/src/plugins/audioruntime"
	"github.com/fnMrRice/MusicFS-server/src/plugins/codecsettings"
	"github.com/fnMrRice/MusicFS-server/src/plugins/dspsettings"
)

func (s *Service) processorForUser(ctx context.Context, owner string, processor audioruntime.Processor, client ...bool) (audioruntime.Processor, error) {
	processor = audioruntime.Safe(processor)
	withoutEffects := s.config.DisableEffects || processor.EffectsDisabled() || len(client) > 0 && client[0]
	withoutChannels := s.config.DisableChannelMix || processor.ChannelMixDisabled() || len(client) > 0 && client[0]
	processor = processor.SelectPolicy(withoutEffects, withoutChannels)
	options, err := dspsettings.Load(ctx, s.repo, owner)
	if err != nil || options == nil {
		return processor, err
	}
	if withoutEffects {
		// 旧效果参数不参与服务端处理。
		options.Chain = nil
	}
	if withoutChannels {
		options.OutputLayout = "stereo"
		options.Chain = dspsettings.WithoutChannelMixNodes(options.Chain)
	}
	if !processor.Available() {
		// 空立体声链不要求处理；是否必须转码仍由播放能力、用户增益和部署策略决定。
		if len(options.Chain) == 0 && options.OutputLayout == "stereo" {
			return processor, nil
		}
		return nil, domain.Fail("UNSUPPORTED", "Audio processor is unavailable for the selected DSP settings")
	}
	return processor.SelectSettings(ctx, *options)
}

// DSPSettingsConfig 每次请求都读取当前实例，插件启停后不会继续使用旧能力。
func (s *Service) DSPSettingsConfig() dspsettings.Config {
	processor, _ := s.audioSnapshot()
	return dspsettings.Config{Defaults: processor.Defaults(), Plugins: processor.DSPPlugins(), Available: processor.Available(),
		DisableEffects:    s.config.DisableEffects || processor.EffectsDisabled(),
		DisableChannelMix: s.config.DisableChannelMix || processor.ChannelMixDisabled(),
		OutputLayouts:     processor.AvailableOutputLayouts(), Validate: processor.ValidateSettings}
}

func (s *Service) recoveryProfileForSession(ctx context.Context, owner string, a *session) (timeline.Profile, error) {
	processor, _ := s.audioSnapshot()
	if processor.DeploymentIdentityValue() != a.processor.DeploymentIdentityValue() {
		return timeline.Profile{}, domain.Fail("UNSUPPORTED", "Audio deployment changed after playback creation")
	}
	output, err := codecsettings.DecodeOutput(ctx, s.repo, owner, processor.AudioDecoder().PluginID)
	if err != nil {
		return timeline.Profile{}, err
	}
	return recoveryProfile(a.processor, output)
}
