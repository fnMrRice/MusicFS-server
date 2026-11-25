package playback

import (
	"context"
	"encoding/json"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

func playbackExpiry(loginExpiry time.Time) time.Time {
	expiry := time.Now().UTC().Add(30 * time.Minute)
	if loginExpiry.Before(expiry) {
		return loginExpiry
	}
	return expiry
}

// KeepAlive 只延长当前登录会话拥有的播放资源，保留原音频流和播放尝试。
func (s *Service) KeepAlive(ctx context.Context, who auth.Session, id string) (time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, err := s.find(who, id)
	if err != nil {
		return time.Time{}, err
	}
	login, err := s.repo.FindAuth(ctx, who.ID)
	if err != nil || login.User.ID != a.user || !time.Now().Before(login.ExpiresAt) {
		return time.Time{}, domain.Fail("AUTH_REQUIRED", "Authentication session expired or was revoked")
	}
	description := a.description
	expiry := playbackExpiry(login.ExpiresAt)
	if !expiry.After(description.ExpiresAt) {
		return expiry, nil
	}
	description.ExpiresAt = expiry
	raw, err := json.Marshal(description)
	if err != nil {
		return time.Time{}, err
	}
	if err := s.repo.PlaybackSave(ctx, id, a.owner, a.user, raw, "active", expiry); err != nil {
		return time.Time{}, err
	}
	a.description.ExpiresAt = expiry
	return expiry, nil
}
