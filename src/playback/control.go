package playback

import (
	"context"
	"encoding/json"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/auth"
	"github.com/fnMrRice/MusicFS-server/src/domain"
)

func (s *Service) Control(ctx context.Context, who auth.Session, id, operation string, r ControlRequest, key string) (Description, error) {
	if err := checkKey(key); err != nil {
		return Description{}, err
	}
	if r.AttemptID == "" || r.PositionMs < 0 {
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
	a, err := s.find(who, id)
	if err != nil {
		return Description{}, err
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
		return d, err
	}
	if a.description.AttemptID != r.AttemptID {
		return Description{}, domain.Fail("CONFLICT", "Playback attempt is no longer current")
	}
	if a.description.SeekableRange == nil || r.PositionMs >= a.description.SeekableRange.EndMs {
		return Description{}, domain.Fail("INVALID_ARGUMENT", "Position is outside the verified track range")
	}
	if a.description.DeliveryMode != "processed" {
		return Description{}, domain.Fail("RESTART_REQUIRED", "Use the original stream timeline or restart playback")
	}
	if operation == "recover" {
		if r.ObservedError == nil {
			return Description{}, domain.Fail("INVALID_ARGUMENT", "Observed error is required")
		}
		if a.recoveryCount >= 2 || (!a.recoveryStarted.IsZero() && time.Since(a.recoveryStarted) > time.Minute) {
			return Description{}, domain.Fail("RECOVERY_EXHAUSTED", "Recovery budget exhausted; restart playback")
		}
	}
	d := a.description
	d.AttemptID = domain.NewID()
	d.RepresentationID = domain.NewID()
	d.PositionOriginMs = r.PositionMs
	d.StreamURL = "/api/v1/playback/sessions/" + id + "/attempts/" + d.AttemptID + "/stream"
	raw, _ := json.Marshal(d)
	if err = s.repo.CommitPlayback(ctx, id, a.owner, a.user, raw, d.ExpiresAt, route, key, digest); err != nil {
		return Description{}, err
	}
	a.cancelAttempt()
	a.attemptCtx, a.cancelAttempt = context.WithCancel(a.ctx)
	a.description = d
	a.lastAccess = time.Now()
	if operation == "recover" {
		a.recoveryCount++
		if a.recoveryStarted.IsZero() {
			a.recoveryStarted = time.Now()
		}
	}
	return d, nil
}
