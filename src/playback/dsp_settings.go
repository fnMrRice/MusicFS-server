package playback

import (
	"context"

	audioffmpeg "github.com/fnMrRice/MusicFS-server/plugins/decoder/ffmpeg"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/playback/timeline"
	"github.com/fnMrRice/MusicFS-server/src/plugins/codecsettings"
	"github.com/fnMrRice/MusicFS-server/src/plugins/dspsettings"
)

func (s *Service) processorForUser(ctx context.Context, owner string, processor *audioffmpeg.Processor) (*audioffmpeg.Processor, error) {
	options, err := dspsettings.Load(ctx, s.repo, owner)
	if err != nil || options == nil {
		return processor, err
	}
	if processor == nil {
		// 空立体声链不要求处理；是否必须转码仍由播放能力、用户增益和部署策略决定。
		if len(options.Chain) == 0 && options.OutputLayout == "stereo" {
			return nil, nil
		}
		return nil, domain.Fail("UNSUPPORTED", "Audio processor is unavailable for the selected DSP settings")
	}
	return processor.WithSettings(ctx, *options)
}

// DSPSettingsConfig 每次请求都读取当前实例，插件启停后不会继续使用旧能力。
func (s *Service) DSPSettingsConfig() dspsettings.Config {
	processor, _ := s.audioSnapshot()
	return dspsettings.Config{Defaults: processor.Defaults(), Plugins: processor.DSPPlugins(), Available: processor != nil,
		OutputLayouts: processor.AvailableOutputLayouts(), Validate: processor.ValidateSettings}
}

func (s *Service) recoveryProfileForSession(ctx context.Context, owner string, a *session) (timeline.Profile, error) {
	processor, _ := s.audioSnapshot()
	if processor.DeploymentIdentity() != a.processor.DeploymentIdentity() {
		return timeline.Profile{}, domain.Fail("UNSUPPORTED", "Audio deployment changed after playback creation")
	}
	output, err := codecsettings.DecodeOutput(ctx, s.repo, owner)
	if err != nil {
		return timeline.Profile{}, err
	}
	return recoveryProfile(a.processor, output)
}
