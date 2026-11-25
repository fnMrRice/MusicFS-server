package playback

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/playback/timeline"
)

func (s *Service) Control(ctx context.Context, who auth.Session, id, operation string, r ControlRequest, key string) (Description, error) {
	applied := false
	defer func() {
		if applied {
			slog.InfoContext(ctx, "Playback control applied", "sessionId", id, "operation", operation, "positionMs", r.PositionMs)
		}
	}()
	if err := checkKey(key); err != nil {
		return Description{}, err
	}
	if r.AttemptID == "" || r.PositionMs < 0 || r.PositionMs > timeline.MaxPositionMs {
		return Description{}, domain.Fail("INVALID_ARGUMENT", "Invalid playback control request")
	}
	if operation != "seek" && operation != "recover" {
		return Description{}, domain.Fail("UNSUPPORTED", "Control operation is unsupported")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx.Err() != nil {
		return Description{}, domain.Fail("TEMPORARY_FAILURE", "Server is draining")
	}
	if err := s.liveAuth(ctx, who); err != nil {
		return Description{}, err
	}
	a, err := s.find(who, id)
	if err != nil {
		return Description{}, err
	}
	if a.sourceLease != nil {
		ctx = a.sourceLease.WithContext(ctx)
	}
	route := "playback/" + id + "/" + operation
	digest := requestDigest(r)
	prior, err := s.repo.Result(ctx, who.ID, route, key, digest)
	if err != nil {
		return Description{}, err
	}
	if len(prior) > 0 {
		var d Description
		err = json.Unmarshal(prior, &d)
		if err == nil && d.AttemptID != a.description.AttemptID {
			return Description{}, domain.Fail("CONFLICT", "Playback result was superseded")
		}
		d.ExpiresAt = a.description.ExpiresAt
		return d, err
	}
	if a.description.AttemptID != r.AttemptID {
		return Description{}, domain.Fail("CONFLICT", "Playback attempt is no longer current")
	}
	if a.online {
		return Description{}, domain.Fail("RESTART_REQUIRED", "Online audio has no verified byte or timeline version; restart playback")
	}
	if a.description.SeekableRange == nil || r.PositionMs < a.description.SeekableRange.StartMs || r.PositionMs >= a.description.SeekableRange.EndMs {
		return Description{}, domain.Fail("INVALID_ARGUMENT", "Position is outside the verified track range")
	}
	if operation == "recover" {
		if !validObserved(r.ObservedError) {
			return Description{}, domain.Fail("INVALID_ARGUMENT", "Observed error is required")
		}
		if err := a.recovery.Take(time.Now(), a.track.ID, true); err != nil {
			return Description{}, err
		}
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, a.recovery.Deadline())
		defer cancel()
		current, err := s.library.Track(ctx, a.user, a.track.ID)
		if err != nil {
			return Description{}, err
		}
		if target(current) != target(a.track) {
			return Description{}, domain.Fail("RESTART_REQUIRED", "Source asset changed; the previous offset cannot be reused")
		}
	}
	if a.description.DeliveryMode != "processed" {
		return Description{}, domain.Fail("RESTART_REQUIRED", "Use the original stream timeline or restart playback")
	}
	d := a.description
	d.AttemptID = domain.NewID()
	d.RepresentationID = domain.NewID()
	d.PositionOriginMs = r.PositionMs
	d.StreamURL = "/api/v1/playback/sessions/" + id + "/attempts/" + d.AttemptID + "/stream"
	raw, _ := json.Marshal(d)
	if operation == "recover" {
		repo, storageErr := s.timelineRepo()
		if storageErr != nil {
			return Description{}, storageErr
		}
		err = repo.CommitRecovery(ctx, timeline.Commit{SessionID: id, AuthID: a.owner, Owner: a.user, PreviousAttemptID: r.AttemptID, Check: timeline.Check{From: target(a.track), To: target(a.track), PositionMs: r.PositionMs}, Continue: true, Description: raw, Result: raw, ExpiresAt: d.ExpiresAt, Route: route, Key: key, Digest: digest})
	} else {
		err = s.repo.CommitPlayback(ctx, id, a.owner, a.user, raw, d.ExpiresAt, route, key, digest, "", "")
	}
	if err != nil {
		return Description{}, err
	}
	a.cancelAttempt()
	a.cachePin.Close()
	a.cachePin = nil
	a.attemptCtx, a.cancelAttempt = context.WithCancel(a.ctx)
	a.description = d
	applied = true
	return d, nil
}
