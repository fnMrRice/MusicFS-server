package playback

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/fnMrRice/MusicFS-server/src/auth"
	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"io"
	"math"
	"mime"
	"os"
	"path/filepath"
	"time"
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
		if _, err = s.find(who, d.SessionID); err != nil {
			s.mu.Unlock()
			return d, domain.Fail("SESSION_EXPIRED", "Playback result expired; create a new operation")
		}
		s.mu.Unlock()
		return d, nil
	}
	if len(s.sessions) >= s.config.MaxSessions {
		s.mu.Unlock()
		return Description{}, domain.Fail("RATE_LIMITED", "Active playback limit reached")
	}
	availableBytes := s.config.MaxCacheBytes - s.used
	s.preparing[who.ID] = cancelPreparation
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.preparing, who.ID)
		s.mu.Unlock()
	}()
	t, err := s.library.Track(ctx, who.User.ID, r.SourceTrackID)
	if err != nil {
		return Description{}, err
	}
	if t.Availability != "available" {
		return Description{}, domain.Fail("SOURCE_OFFLINE", "Track is unavailable")
	}
	processed := r.Preferences.ForceProcessing || r.Preferences.GainDB != 0 || !originalCandidate(t) || !supports(r.Capabilities, t.MIME)
	if processed && (s.processor == nil || !supports(r.Capabilities, "audio/mpeg")) {
		return Description{}, domain.Fail("UNSUPPORTED", "No compatible audio processing profile is available")
	}
	if t.Size <= 0 {
		return Description{}, domain.Fail("UNSUPPORTED", "Source contains no audio data")
	}
	if t.Size > availableBytes {
		return Description{}, domain.Fail("RATE_LIMITED", "Source exceeds the cache budget")
	}
	id := domain.NewID()
	handle, err := s.library.Open(ctx, t, id)
	if err != nil {
		return Description{}, err
	}
	defer handle.Close()
	defer s.library.Revoke(t.ProviderID, id)
	path := filepath.Join(s.config.CacheRoot, id+".bin")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return Description{}, domain.Fail("TEMPORARY_FAILURE", "Cannot prepare playback representation")
	}
	success := false
	defer func() {
		f.Close()
		if !success {
			os.Remove(path)
		}
	}()
	hash := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, hash), io.LimitReader(handle, t.Size+1))
	if err != nil || n != t.Size {
		return Description{}, domain.Fail("RESOURCE_CHANGED", "Source changed while preparing playback")
	}
	if err = handle.Validate(); err != nil {
		return Description{}, domain.Fail("RESOURCE_CHANGED", "Source changed while preparing playback")
	}
	if err = f.Close(); err != nil {
		return Description{}, err
	}
	if err = ctx.Err(); err != nil {
		return Description{}, err
	}
	if !processed && t.MIME == "audio/wav" {
		if err := inspectWave(path, &t); err != nil {
			return Description{}, err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx.Err() != nil {
		return Description{}, domain.Fail("TEMPORARY_FAILURE", "Server is draining")
	}
	if err := ctx.Err(); err != nil {
		return Description{}, err
	}
	if !time.Now().Before(who.ExpiresAt) {
		return Description{}, domain.Fail("AUTH_REQUIRED", "Authentication session expired")
	}
	lifetime, cancel := context.WithCancel(s.ctx)
	a := &session{owner: who.ID, user: who.User.ID, path: path, etag: fmt.Sprintf("\"%x\"", hash.Sum(nil)), size: n, track: t, gain: r.Preferences.GainDB, ctx: lifetime, cancel: cancel, lastAccess: time.Now()}
	expiry := time.Now().UTC().Add(30 * time.Minute)
	if who.ExpiresAt.Before(expiry) {
		expiry = who.ExpiresAt
	}
	d := Description{SessionID: id, AssetID: t.AssetID, DeliveryMode: "proxy", OutputProfile: OutputProfile{MIME: t.MIME, Codec: t.Codec, Container: t.Container, Transport: "htmlAudio"}, ExpiresAt: expiry, SeekMode: "byteRange", EffectiveProcessing: []Processing{}, ResumeDecision: "continue"}
	if t.DurationMs != nil {
		d.SeekableRange = &Range{EndMs: *t.DurationMs}
	}
	if processed {
		codec, container := "mp3", "mp3"
		d.DeliveryMode = "processed"
		d.OutputProfile = OutputProfile{MIME: "audio/mpeg", Codec: &codec, Container: &container, Transport: "htmlAudio"}
		d.SeekMode = "none"
		if t.DurationMs != nil {
			d.SeekMode = "timeRestart"
		}
		d.EffectiveProcessing = []Processing{{Type: "gain", GainDB: a.gain, Version: "1", Execution: "server"}}
	}
	a.description = d
	s.nextAttempt(a, 0)
	raw, _ := json.Marshal(a.description)
	if err = s.repo.CommitPlayback(ctx, id, who.ID, who.User.ID, raw, expiry, "playback/create", key, digest); err != nil {
		cancel()
		return Description{}, err
	}
	s.sessions[id] = a
	s.used += n
	success = true
	return a.description, nil
}
func (s *Service) nextAttempt(a *session, position int64) {
	if a.cancelAttempt != nil {
		a.cancelAttempt()
	}
	a.attemptCtx, a.cancelAttempt = context.WithCancel(a.ctx)
	a.description.AttemptID = domain.NewID()
	a.description.RepresentationID = domain.NewID()
	a.description.PositionOriginMs = position
	a.description.StreamURL = "/api/v1/playback/sessions/" + a.description.SessionID + "/attempts/" + a.description.AttemptID + "/stream"
}
