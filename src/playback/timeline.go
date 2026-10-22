package playback

import (
	"context"
	"time"

	"github.com/fnMrRice/MusicFS-server/plugins/decoder/ffmpeg"
	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/playback/timeline"
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
	output, err := codecsettings.DecodeOutput(ctx, s.repo, owner)
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

func recoveryProfile(processor *audioffmpeg.Processor, outputs ...string) (timeline.Profile, error) {
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
	if processor != nil {
		p.DecoderSHA256 = processor.Capabilities.FFmpegSHA256
		if err := processor.SupportsDecodeOutput(decodeOutput); err != nil {
			return p, domain.Fail("UNSUPPORTED", "A verified decoder output format is unavailable")
		}
		if revision := processor.ProcessingIdentityForDecodeOutput(decodeOutput).PipelineRevision; revision != "" {
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
	if !s.config.DisableEffects && !processor.EffectsDisabled() {
		profile.GainDB = r.Profile.GainDB
	}
	if err != nil || r.Profile != profile {
		// 客户端模式的证据必须绑定无效果身份与零服务端增益。
		clientProfile, clientErr := s.RecoveryProfileForUser(ctx, owner, "client")
		clientProfile = matchRecoveryBitrate(clientProfile, r.Profile.OutputProfile)
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
	base, baseOK := audioffmpeg.ProfileByID(expected.OutputProfile)
	selected, selectedOK := audioffmpeg.ProfileByID(requested)
	if baseOK && selectedOK && selected.Codec == base.Codec && selected.Layout == base.Layout {
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
