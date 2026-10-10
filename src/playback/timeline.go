package playback

import (
	"context"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/auth"
	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/timeline"
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
	p := timeline.Profile{OutputProfile: "mp3-128", ProcessingRevision: "gain-v1"}
	if s.processor != nil {
		p.DecoderSHA256 = s.processor.Capabilities.FFmpegSHA256
	}
	if err := p.Validate(); err != nil {
		return p, domain.Fail("UNSUPPORTED", "A verified recovery processing profile is unavailable")
	}
	return p, nil
}
func (s *Service) AddTimeline(ctx context.Context, owner string, r timeline.Request, key string) (timeline.Evidence, error) {
	profile, err := s.RecoveryProfile()
	if err != nil {
		return timeline.Evidence{}, err
	}
	profile.GainDB = r.Profile.GainDB
	if r.Profile != profile {
		return timeline.Evidence{}, domain.Fail("INVALID_ARGUMENT", "Timeline profile differs from the current processing configuration")
	}
	repo, err := s.timelineRepo()
	if err != nil {
		return timeline.Evidence{}, err
	}
	return repo.CreateTimeline(ctx, owner, r, key)
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
