package playback

import (
	"context"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/playback/timeline"
	"github.com/fnMrRice/MusicFS-server/src/plugins/audioruntime"
	"github.com/fnMrRice/MusicFS-server/src/plugins/codecsettings"
)

func target(t library.Track) timeline.Target {
	return timeline.Target{SourceTrackID: t.ID, AssetID: t.AssetID, Version: t.Version}
}
func (s *Service) timelineRepo() (timeline.Repository, error) {
	repo, ok := s.repo.(timeline.Repository)
	if !ok {
		return nil, domain.Fail("UNSUPPORTED", "Timeline evidence storage is unavailable")
	}
	return repo, nil
}
func (s *Service) RecoveryProfile() (timeline.Profile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return recoveryProfile(s.processor)
}

// RecoveryProfileForUser 与新会话读取相同的插件偏好，避免跨量化格式复用对齐证据。
func (s *Service) RecoveryProfileForUser(ctx context.Context, owner string, executions ...string) (timeline.Profile, error) {
	execution := "server"
	if len(executions) > 0 && executions[0] != "" {
		execution = executions[0]
	}
	if execution != "server" && execution != "client" {
		return timeline.Profile{}, domain.Fail("INVALID_ARGUMENT", "DSP execution must be server or client")
	}
	current, _ := s.audioSnapshot()
	output, err := codecsettings.DecodeOutput(ctx, s.repo, owner, current.AudioDecoder().PluginID)
	if err != nil {
		return timeline.Profile{}, err
	}
	s.mu.Lock()
	processor := s.processor
	s.mu.Unlock()
	processor, err = s.processorForUser(ctx, owner, processor, execution == "client")
	if err != nil {
		return timeline.Profile{}, err
	}
	return recoveryProfile(processor, output)
}

func decodePlanOutput(output string) string {
	if output == "native" {
		return ""
	}
	return output
}

func recoveryProfile(processor audioruntime.Processor, outputs ...string) (timeline.Profile, error) {
	processor = audioruntime.Safe(processor)
	p := timeline.Profile{ProcessingRevision: "gain-v1"}
	decodeOutput := ""
	if len(outputs) > 0 {
		decodeOutput = decodePlanOutput(outputs[0])
	}
	output, err := processor.OutputProfile()
	if err != nil {
		return p, domain.Fail("UNSUPPORTED", "A verified recovery processing profile is unavailable")
	}
	p.OutputProfile = output.ID
	if processor.Available() {
		p.DecoderSHA256 = processor.RuntimeCapabilities().DecoderSHA256
		if err := processor.SupportsDecodeOutput(decodeOutput); err != nil {
			return p, domain.Fail("UNSUPPORTED", "A verified decoder output format is unavailable")
		}
		if revision := processor.RuntimeIdentity(decodeOutput).PipelineRevision; revision != "" {
			p.ProcessingRevision = revision
		}
	}
	if err := p.Validate(); err != nil {
		return p, domain.Fail("UNSUPPORTED", "A verified recovery processing profile is unavailable")
	}
	return p, nil
}
func (s *Service) AddTimeline(ctx context.Context, owner string, r timeline.Request, key string) (timeline.Evidence, error) {
	profile, err := s.RecoveryProfileForUser(ctx, owner)
	profile = matchRecoveryBitrate(profile, r.Profile.OutputProfile)
	processor, _ := s.audioSnapshot()
	if s.config.DisableChannelMix || processor.ChannelMixDisabled() {
		profile = matchRecoveryChannels(profile, r.Profile.OutputProfile, processor)
	}
	if !s.config.DisableEffects && !processor.EffectsDisabled() {
		profile.GainDB = r.Profile.GainDB
	}
	if err != nil || r.Profile != profile {
		// 客户端模式的证据必须绑定无效果身份与零服务端增益。
		clientProfile, clientErr := s.RecoveryProfileForUser(ctx, owner, "client")
		clientProfile = matchRecoveryBitrate(clientProfile, r.Profile.OutputProfile)
		clientProfile = matchRecoveryChannels(clientProfile, r.Profile.OutputProfile, processor)
		if clientErr != nil || r.Profile != clientProfile {
			if err != nil {
				return timeline.Evidence{}, err
			}
			return timeline.Evidence{}, domain.Fail("INVALID_ARGUMENT", "Timeline profile differs from the current processing configuration")
		}
	}
	repo, err := s.timelineRepo()
	if err != nil {
		return timeline.Evidence{}, err
	}
	return repo.CreateTimeline(ctx, owner, r, key)
}

// 人工对齐证据可绑定会话所选码率，仍须匹配当前部署编码、布局和处理身份。
func matchRecoveryBitrate(expected timeline.Profile, requested string) timeline.Profile {
	base, baseOK := audioruntime.LookupProfile(expected.OutputProfile)
	selected, selectedOK := audioruntime.LookupProfile(requested)
	if baseOK && selectedOK && selected.Codec == base.Codec && selected.Layout == base.Layout {
		expected.OutputProfile = requested
	}
	return expected
}

// 来源声道由会话选择，人工证据仍必须绑定已探测编码能力与同一无扩展身份。
func matchRecoveryChannels(expected timeline.Profile, requested string, processor audioruntime.Processor) timeline.Profile {
	selected, valid := processor.LookupProfile(requested)
	if valid && processor.SupportsProfile(selected) {
		expected.OutputProfile = requested
	}
	return expected
}
func (s *Service) Timeline(ctx context.Context, owner, id string) (timeline.Evidence, error) {
	repo, err := s.timelineRepo()
	if err != nil {
		return timeline.Evidence{}, err
	}
	return repo.Timeline(ctx, owner, id)
}
func (s *Service) RevokeTimeline(ctx context.Context, owner, id string, revision int64, key string) (timeline.Evidence, error) {
	repo, err := s.timelineRepo()
	if err != nil {
		return timeline.Evidence{}, err
	}
	return repo.RevokeTimeline(ctx, owner, id, revision, key)
}
func (s *Service) liveAuth(ctx context.Context, who auth.Session) error {
	fresh, err := s.repo.FindAuth(ctx, who.ID)
	if err != nil || fresh.User.ID != who.User.ID || !time.Now().Before(fresh.ExpiresAt) {
		return domain.Fail("AUTH_REQUIRED", "Authentication session expired or was revoked")
	}
	return nil
}
