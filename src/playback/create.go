package playback

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"mime"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
)

func supports(c Capabilities, wanted string) bool {
	for _, f := range c.Formats {
		base, _, err := mime.ParseMediaType(f.MIME)
		if err == nil && base == wanted && (f.CanPlay == "maybe" || f.CanPlay == "probably") {
			return true
		}
	}
	return false
}

func originalCandidate(t library.Track) bool {
	if t.MIME == "audio/wav" {
		return t.Codec == nil || *t.Codec == "pcm_s16le"
	}
	return t.MIME == "audio/mpeg" && t.Codec != nil && *t.Codec == "mp3" && t.Container != nil && *t.Container == "mp3"
}
func requestDigest(value any) string {
	raw, _ := json.Marshal(value)
	return fmt.Sprintf("%x", sha256.Sum256(raw))
}
func checkKey(key string) error {
	if len(key) < 8 || len(key) > 128 {
		return domain.Fail("INVALID_ARGUMENT", "Idempotency-Key must contain 8 to 128 characters")
	}
	return nil
}

func (s *Service) Create(ctx context.Context, who auth.Session, r CreateRequest, key string) (Description, error) {
	if err := checkKey(key); err != nil {
		return Description{}, err
	}
	if r.SourceTrackID == "" || !r.Capabilities.HTMLAudio || len(r.Capabilities.Formats) > 32 || math.IsNaN(r.Preferences.GainDB) || r.Preferences.GainDB < -60 || r.Preferences.GainDB > 0 {
		return Description{}, domain.Fail("INVALID_ARGUMENT", "Invalid playback capabilities or preferences")
	}
	select {
	case s.creation <- struct{}{}:
		defer func() { <-s.creation }()
	case <-ctx.Done():
		return Description{}, ctx.Err()
	case <-s.ctx.Done():
		return Description{}, domain.Fail("TEMPORARY_FAILURE", "Server is draining")
	}
	ctx, cancelPreparation := context.WithCancel(ctx)
	unlink := context.AfterFunc(s.ctx, cancelPreparation)
	defer func() { unlink(); cancelPreparation() }()
	s.mu.Lock()
	if s.ctx.Err() != nil {
		s.mu.Unlock()
		return Description{}, domain.Fail("TEMPORARY_FAILURE", "Server is draining")
	}
	fresh, err := s.repo.FindAuth(ctx, who.ID)
	if err != nil || fresh.User.ID != who.User.ID || !time.Now().Before(fresh.ExpiresAt) {
		s.mu.Unlock()
		return Description{}, domain.Fail("AUTH_REQUIRED", "Authentication session expired or was revoked")
	}
	who = fresh
	digest := requestDigest(r)
	prior, err := s.repo.Result(ctx, who.ID, "playback/create", key, digest)
	if err != nil {
		s.mu.Unlock()
		return Description{}, err
	}
	if len(prior) > 0 {
		var d Description
		if err := json.Unmarshal(prior, &d); err != nil {
			s.mu.Unlock()
			return Description{}, err
		}
		current, currentErr := s.find(who, d.SessionID)
		if currentErr != nil {
			s.mu.Unlock()
			return d, domain.Fail("SESSION_EXPIRED", "Playback result expired; create a new operation")
		}
		if current.description.AttemptID != d.AttemptID {
			s.mu.Unlock()
			return Description{}, domain.Fail("CONFLICT", "Playback result was superseded")
		}
		s.mu.Unlock()
		return d, nil
	}
	if len(s.sessions) >= s.config.MaxSessions {
		s.mu.Unlock()
		return Description{}, domain.Fail("RATE_LIMITED", "Active playback limit reached")
	}
	s.preparing[who.ID] = cancelPreparation
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.preparing, who.ID)
		s.mu.Unlock()
	}()
	id := domain.NewID()
	prepared, err := s.prepare(ctx, who.User.ID, id, r)
	if err != nil {
		return Description{}, err
	}
	transferred := false
	defer func() {
		if !transferred {
			prepared.discard()
		}
	}()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx.Err() != nil {
		return Description{}, domain.Fail("TEMPORARY_FAILURE", "Server is draining")
	}
	if err := ctx.Err(); err != nil {
		return Description{}, err
	}
	fresh, err = s.repo.FindAuth(ctx, who.ID)
	if err != nil || fresh.User.ID != who.User.ID || !time.Now().Before(fresh.ExpiresAt) {
		return Description{}, domain.Fail("AUTH_REQUIRED", "Authentication session expired or was revoked")
	}
	if err := prepared.checkLeases(); err != nil {
		return Description{}, err
	}
	capabilities := r.Capabilities
	capabilities.Formats = append([]Format(nil), r.Capabilities.Formats...)
	a := &session{owner: who.ID, user: who.User.ID, path: prepared.path, etag: prepared.etag, size: prepared.size, track: prepared.track, gain: r.Preferences.GainDB, capabilities: capabilities, lastAccess: time.Now(), sourceLease: prepared.lease}
	s.installPluginLeases(a, prepared)
	expiry := time.Now().UTC().Add(30 * time.Minute)
	if who.ExpiresAt.Before(expiry) {
		expiry = who.ExpiresAt
	}
	d := prepared.describe(id, a.gain, expiry)
	a.description = d
	s.nextAttempt(a, 0)
	raw, _ := json.Marshal(a.description)
	if err = s.repo.CommitPlayback(ctx, id, who.ID, who.User.ID, raw, expiry, "playback/create", key, digest); err != nil {
		a.cancel()
		a.releasePluginLeases()
		return Description{}, err
	}
	s.sessions[id] = a
	transferred = true
	return a.description, nil
}
func (s *Service) nextAttempt(a *session, position int64) {
	a.cachePin.Close()
	a.cachePin = nil
	if a.cancelAttempt != nil {
		a.cancelAttempt()
	}
	a.attemptCtx, a.cancelAttempt = context.WithCancel(a.ctx)
	a.description.AttemptID = domain.NewID()
	a.description.RepresentationID = domain.NewID()
	a.description.PositionOriginMs = position
	a.description.StreamURL = "/api/v1/playback/sessions/" + a.description.SessionID + "/attempts/" + a.description.AttemptID + "/stream"
}
