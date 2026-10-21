package playback

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/playback/timeline"
)

type RecoverSourceRequest struct {
	AttemptID           string         `json:"attemptId"`
	TargetSourceTrackID string         `json:"targetSourceTrackId"`
	PositionMs          int64          `json:"positionMs"`
	EvidenceID          *string        `json:"evidenceId"`
	ObservedError       *ObservedError `json:"observedError"`
}
type RecoveryResult struct {
	Decision            string       `json:"decision"`
	Reason              string       `json:"reason"`
	TargetSourceTrackID string       `json:"targetSourceTrackId"`
	Playback            *Description `json:"playback"`
}

func validObserved(observed *ObservedError) bool {
	return observed != nil && len(observed.Code) > 0 && len(observed.Code) <= 128 && len(observed.Message) <= 1024
}

func (s *Service) RecoverSource(ctx context.Context, who auth.Session, id string, r RecoverSourceRequest, key string) (RecoveryResult, error) {
	var empty RecoveryResult
	if err := checkKey(key); err != nil {
		return empty, err
	}
	if r.AttemptID == "" || len(r.AttemptID) > 128 || r.TargetSourceTrackID == "" || len(r.TargetSourceTrackID) > 128 || r.PositionMs < 0 || r.PositionMs > timeline.MaxPositionMs || !validObserved(r.ObservedError) || r.EvidenceID != nil && (*r.EvidenceID == "" || len(*r.EvidenceID) > 128) {
		return empty, domain.Fail("INVALID_ARGUMENT", "Invalid cross-source recovery request")
	}
	repo, err := s.timelineRepo()
	if err != nil {
		return empty, err
	}
	select {
	case s.creation <- struct{}{}:
		defer func() { <-s.creation }()
	case <-ctx.Done():
		return empty, ctx.Err()
	case <-s.ctx.Done():
		return empty, domain.Fail("TEMPORARY_FAILURE", "Server is draining")
	}
	s.mu.Lock()
	a, prior, err := s.beginRecovery(ctx, who, id, r, key)
	if err != nil || prior != nil {
		s.mu.Unlock()
		if prior != nil {
			return *prior, nil
		}
		return empty, err
	}
	ctx, cancel := context.WithDeadline(ctx, a.recovery.Deadline())
	unlink := context.AfterFunc(a.ctx, cancel)
	s.preparing[who.ID] = cancel
	from, capabilities, gain := target(a.track), a.capabilities, a.gain
	fromProfile, fromProfileErr := recoveryProfile(a.processor)
	fromProfile.GainDB = gain
	s.mu.Unlock()
	defer func() { unlink(); cancel(); s.mu.Lock(); delete(s.preparing, who.ID); s.mu.Unlock() }()
	t, err := s.library.Track(ctx, who.User.ID, r.TargetSourceTrackID)
	if err != nil {
		return empty, err
	}
	// Even a prompt-only candidate requires current ownership, provider access
	// and a valid source grant; a known recording is not a permission grant.
	grant := domain.NewID()
	defer s.library.Revoke(t.ProviderID, grant)
	handle, err := s.library.Open(ctx, t, grant)
	if err != nil {
		return empty, err
	}
	if err = handle.Validate(); err != nil {
		handle.Close()
		return empty, domain.Fail("RESOURCE_CHANGED", "Recovery source changed")
	}
	handle.Close()
	profile, profileErr := s.RecoveryProfile()
	profile.GainDB = gain
	check := timeline.Check{From: from, To: target(t), PositionMs: r.PositionMs, Profile: profile}
	if r.EvidenceID != nil {
		check.EvidenceID = *r.EvidenceID
	}
	evidence, checkErr := repo.CheckTimeline(ctx, who.User.ID, check)
	reason := "TIMELINE_EVIDENCE_REQUIRED"
	if checkErr != nil {
		var failure *domain.Error
		if !errors.As(checkErr, &failure) || failure.Code != "RESTART_REQUIRED" {
			return empty, checkErr
		}
		reason = "TIMELINE_EVIDENCE_NOT_APPLICABLE"
	}
	if r.EvidenceID == nil {
		reason = "TIMELINE_EVIDENCE_REQUIRED"
	}
	if profileErr != nil || fromProfileErr != nil || profile != fromProfile || !supports(capabilities, "audio/mpeg") {
		reason = "OUTPUT_PROFILE_UNAVAILABLE"
		checkErr = domain.Fail("RESTART_REQUIRED", "A matching recovery processing profile is unavailable")
	}
	result := RecoveryResult{Decision: "promptRestart", Reason: reason, TargetSourceTrackID: r.TargetSourceTrackID}
	var prepared *preparedInput
	var position int64
	if checkErr == nil {
		position, err = evidence.Map(check)
		if err != nil {
			return empty, err
		}
		prepared, err = s.prepare(ctx, who.User.ID, domain.NewID(), CreateRequest{SourceTrackID: r.TargetSourceTrackID, Capabilities: capabilities, Preferences: Preferences{ForceProcessing: true, GainDB: gain}})
		if err != nil {
			return empty, err
		}
		defer func() {
			if prepared != nil {
				prepared.discard()
			}
		}()
		if target(prepared.track) != check.To || prepared.track.DurationMs == nil || position >= *prepared.track.DurationMs {
			return empty, domain.Fail("RESOURCE_CHANGED", "Recovery target changed during preparation")
		}
		preparedProfile, preparedProfileErr := recoveryProfile(prepared.processor)
		preparedProfile.GainDB = gain
		if preparedProfileErr != nil || preparedProfile != profile {
			return empty, domain.Fail("RESOURCE_CHANGED", "Recovery processor changed during preparation")
		}
		result.Decision, result.Reason = "continue", "MANUAL_ALIGNMENT"
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, err := s.find(who, id)
	if err != nil {
		return empty, err
	}
	if current != a || a.description.AttemptID != r.AttemptID {
		return empty, domain.Fail("CONFLICT", "Playback attempt changed during recovery")
	}
	if err = ctx.Err(); err != nil {
		return empty, err
	}
	if prepared != nil {
		if err = prepared.checkLeases(); err != nil {
			return empty, err
		}
	}
	commit := timeline.Commit{SessionID: id, AuthID: who.ID, Owner: who.User.ID, PreviousAttemptID: r.AttemptID, Check: check, Continue: prepared != nil, ExpiresAt: a.description.ExpiresAt, Route: "playback/" + id + "/recover-source", Key: key, Digest: requestDigest(r)}
	if prepared != nil {
		d := prepared.describe(id, gain, a.description.ExpiresAt)
		setAttempt(&d, position)
		result.Playback = &d
		commit.Description, _ = json.Marshal(d)
	}
	commit.Result, _ = json.Marshal(result)
	if err = repo.CommitRecovery(ctx, commit); err != nil {
		return empty, err
	}
	if prepared != nil {
		s.installRecovery(a, prepared, *result.Playback)
		prepared = nil
	}
	slog.InfoContext(ctx, "Playback source recovery decided", "sessionId", id, "decision", result.Decision, "reason", result.Reason)
	return result, nil
}

// beginRecovery is under mu; reservations are retained on all later failures.
func (s *Service) beginRecovery(ctx context.Context, who auth.Session, id string, r RecoverSourceRequest, key string) (*session, *RecoveryResult, error) {
	if s.ctx.Err() != nil {
		return nil, nil, domain.Fail("TEMPORARY_FAILURE", "Server is draining")
	}
	if err := s.liveAuth(ctx, who); err != nil {
		return nil, nil, err
	}
	a, err := s.find(who, id)
	if err != nil {
		return nil, nil, err
	}
	prior, err := s.repo.Result(ctx, who.ID, "playback/"+id+"/recover-source", key, requestDigest(r))
	if err != nil {
		return nil, nil, err
	}
	if len(prior) > 0 {
		var result RecoveryResult
		if err = json.Unmarshal(prior, &result); err != nil {
			return nil, nil, err
		}
		attempt := r.AttemptID
		if result.Playback != nil {
			attempt = result.Playback.AttemptID
		}
		if a.description.AttemptID != attempt {
			return nil, nil, domain.Fail("CONFLICT", "Recovery result was superseded")
		}
		return a, &result, nil
	}
	if a.description.AttemptID != r.AttemptID {
		return nil, nil, domain.Fail("CONFLICT", "Playback attempt is no longer current")
	}
	if a.track.ID == r.TargetSourceTrackID {
		return nil, nil, domain.Fail("INVALID_ARGUMENT", "Use same-source recovery for the current source")
	}
	if a.description.SeekableRange == nil || r.PositionMs < a.description.SeekableRange.StartMs || r.PositionMs >= a.description.SeekableRange.EndMs {
		return nil, nil, domain.Fail("INVALID_ARGUMENT", "Position is outside the verified track range")
	}
	if err = a.recovery.Take(time.Now(), r.TargetSourceTrackID, false); err != nil {
		return nil, nil, err
	}
	return a, nil, nil
}
func (s *Service) installRecovery(a *session, p *preparedInput, d Description) {
	a.cancelAttempt()
	a.cancel()
	a.releasePluginLeases()
	a.cachePin.Close()
	a.cachePin = nil
	s.installPluginLeases(a, p)
	a.attemptCtx, a.cancelAttempt = context.WithCancel(a.ctx)
	if a.sourceLease != nil {
		a.sourceLease.Close()
	}
	s.garbage[a.path] = a.size
	a.path, a.etag, a.size, a.track, a.sourceLease = p.path, p.etag, p.size, p.track, p.lease
	a.description = d
	a.lastAccess = time.Now()
	s.collect()
}
func setAttempt(d *Description, position int64) {
	d.AttemptID = domain.NewID()
	d.RepresentationID = domain.NewID()
	d.PositionOriginMs = position
	d.StreamURL = "/api/v1/playback/sessions/" + d.SessionID + "/attempts/" + d.AttemptID + "/stream"
}
