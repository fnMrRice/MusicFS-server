package playback_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/matching"
	"github.com/fnMrRice/MusicFS-server/src/playback"
)

func TestCrossSourceManualAlignmentPublishesNewAttemptAndRevokesOldBuffer(t *testing.T) {
	f := recoverySetup(t, nil, 0)
	e := f.evidence(t, 1)
	d := f.create(t, "recovery-initial")
	old, err := f.player.OpenStream(context.Background(), f.who, d.SessionID, d.AttemptID, false)
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	r := recoveryRequest(d, f.tracks[1].ID, &e)
	result, err := f.player.RecoverSource(context.Background(), f.who, d.SessionID, r, "recover-directed")
	if err != nil || result.Decision != "continue" || result.Playback == nil {
		t.Fatalf("recovery: %+v %v", result, err)
	}
	next := *result.Playback
	_, createErr := f.player.Create(context.Background(), f.who, f.request, "recovery-initial")
	recoveryError(t, createErr, "CONFLICT")
	if next.SessionID != d.SessionID || next.AttemptID == d.AttemptID || next.RepresentationID == d.RepresentationID || next.AssetID != f.tracks[1].AssetID || next.PositionOriginMs != 5000 || !next.ExpiresAt.Equal(d.ExpiresAt) || next.DeliveryMode != "processed" || next.SeekMode != "timeRestart" {
		t.Fatalf("wrong recovered representation: %+v", next)
	}
	if old.Context.Err() == nil {
		t.Fatal("old stream was not canceled")
	}
	if stale, err := f.player.OpenStream(context.Background(), f.who, d.SessionID, d.AttemptID, false); err == nil {
		stale.Close()
		t.Fatal("old stream URL remained live")
	}
	replay, err := f.player.RecoverSource(context.Background(), f.who, d.SessionID, r, "recover-directed")
	if err != nil || replay.Playback == nil || replay.Playback.AttemptID != next.AttemptID {
		t.Fatalf("recovery replay: %+v %v", replay, err)
	}
	control := playback.ControlRequest{AttemptID: next.AttemptID, PositionMs: 5500}
	if _, err = f.player.Control(context.Background(), f.who, d.SessionID, "seek", control, "after-recovery-seek"); err != nil {
		t.Fatal(err)
	}
	_, err = f.player.RecoverSource(context.Background(), f.who, d.SessionID, r, "recover-directed")
	recoveryError(t, err, "CONFLICT")
	if err = f.player.Stop(context.Background(), f.who, d.SessionID); err != nil {
		t.Fatal(err)
	}
	_, err = f.player.RecoverSource(context.Background(), f.who, d.SessionID, r, "recover-directed")
	recoveryError(t, err, "NOT_FOUND")
}

func TestRecoveryWithoutEvidencePromptsAndBoundsAllCandidateAttempts(t *testing.T) {
	f := recoverySetup(t, nil, 0)
	d := f.create(t, "prompt-initial")
	for i := 1; i <= 3; i++ {
		r := recoveryRequest(d, f.tracks[i].ID, nil)
		key := domain.NewID()
		result, err := f.player.RecoverSource(context.Background(), f.who, d.SessionID, r, key)
		if err != nil || result.Decision != "promptRestart" || result.Playback != nil {
			t.Fatalf("missing evidence: %+v %v", result, err)
		}
		if _, err = f.player.RecoverSource(context.Background(), f.who, d.SessionID, r, key); err != nil {
			t.Fatal("prompt replay failed", err)
		}
		_, err = f.player.RecoverSource(context.Background(), f.who, d.SessionID, r, domain.NewID())
		recoveryError(t, err, "RECOVERY_EXHAUSTED")
	}
	_, err := f.player.RecoverSource(context.Background(), f.who, d.SessionID, recoveryRequest(d, f.tracks[4].ID, nil), "fourth-candidate")
	recoveryError(t, err, "RECOVERY_EXHAUSTED")
	if stream, err := f.player.OpenStream(context.Background(), f.who, d.SessionID, d.AttemptID, false); err != nil {
		t.Fatal("prompt unexpectedly destroyed current stream", err)
	} else {
		stream.Close()
	}
}

func TestTimelineRevocationOwnerAssetRecordingAndProfileChecks(t *testing.T) {
	f := recoverySetup(t, nil, 0)
	e := f.evidence(t, 1)
	ctx := context.Background()
	_, err := f.player.Timeline(ctx, "another-owner", e.ID)
	recoveryError(t, err, "NOT_FOUND")
	changed := e.Request
	changed.To.Version = "obsolete"
	_, err = f.repo.CreateTimeline(ctx, "admin", changed, "obsolete-asset")
	recoveryError(t, err, "RESOURCE_CHANGED")
	changed = e.Request
	changed.Range.EndMs = 7000
	_, err = f.repo.CreateTimeline(ctx, "admin", changed, "invalid-coverage")
	recoveryError(t, err, "INVALID_ARGUMENT")
	changed = e.Request
	changed.Profile.GainDB = -6
	profileEvidence, err := f.player.AddTimeline(ctx, "admin", changed, "other-gain-profile")
	if err != nil {
		t.Fatal(err)
	}
	d := f.create(t, "profile-check-create")
	result, err := f.player.RecoverSource(ctx, f.who, d.SessionID, recoveryRequest(d, f.tracks[1].ID, &profileEvidence), "different-gain")
	if err != nil || result.Decision != "promptRestart" {
		t.Fatalf("different gain accepted: %+v %v", result, err)
	}
	revoked, err := f.player.RevokeTimeline(ctx, "admin", e.ID, 1, "revoke-alignment")
	if err != nil || revoked.RevokedAt == nil || revoked.Revision != 2 {
		t.Fatal("revocation failed", err)
	}
	if _, err = f.player.RevokeTimeline(ctx, "admin", e.ID, 1, "revoke-alignment"); err != nil {
		t.Fatal("revocation replay failed", err)
	}
	d = f.create(t, "revocation-check-create")
	result, err = f.player.RecoverSource(ctx, f.who, d.SessionID, recoveryRequest(d, f.tracks[1].ID, &e), "revoked-evidence")
	if err != nil || result.Decision != "promptRestart" {
		t.Fatalf("revoked evidence accepted: %+v %v", result, err)
	}
	e2 := f.evidence(t, 2)
	if _, err = f.repo.Unlink(ctx, "admin", matching.UnlinkRequest{ExpectedRevision: 1, SourceTrackID: f.tracks[2].ID}, "unlink-before-recovery"); err != nil {
		t.Fatal(err)
	}
	d = f.create(t, "association-check-create")
	result, err = f.player.RecoverSource(ctx, f.who, d.SessionID, recoveryRequest(d, f.tracks[2].ID, &e2), "recording-changed")
	if err != nil || result.Decision != "promptRestart" {
		t.Fatalf("unlinked source continued: %+v %v", result, err)
	}
}

func TestRecoveryPreparationIsCanceledByStopAndDoesNotBlockIt(t *testing.T) {
	f := recoverySetup(t, nil, 0)
	e := f.evidence(t, 1)
	d := f.create(t, "cancel-recovery-create")
	f.repo.gate.Store(true)
	done := make(chan error, 1)
	go func() {
		_, err := f.player.RecoverSource(context.Background(), f.who, d.SessionID, recoveryRequest(d, f.tracks[1].ID, &e), "pending-recovery")
		done <- err
	}()
	select {
	case <-f.repo.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("preparation did not start")
	}
	stopped := make(chan error, 1)
	go func() { stopped <- f.player.Stop(context.Background(), f.who, d.SessionID) }()
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		close(f.repo.proceed)
		t.Fatal("preparation blocked stop")
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("stopped preparation published")
		}
	case <-time.After(time.Second):
		close(f.repo.proceed)
		t.Fatal("preparation did not cancel")
	}
}

func TestRecoveryFailuresConsumeBudgetAndCacheCountsOldAndNew(t *testing.T) {
	f := recoverySetup(t, nil, 1500000)
	e := f.evidence(t, 1)
	d := f.create(t, "cache-budget-create")
	r := recoveryRequest(d, f.tracks[1].ID, &e)
	_, err := f.player.RecoverSource(context.Background(), f.who, d.SessionID, r, "budget-failure")
	recoveryError(t, err, "RATE_LIMITED")
	_, err = f.player.RecoverSource(context.Background(), f.who, d.SessionID, r, "budget-failure")
	recoveryError(t, err, "RECOVERY_EXHAUSTED")
	if stream, err := f.player.OpenStream(context.Background(), f.who, d.SessionID, d.AttemptID, false); err != nil {
		t.Fatal("failed replacement damaged current stream", err)
	} else {
		stream.Close()
	}
	if err = f.player.Stop(context.Background(), f.who, d.SessionID); err != nil {
		t.Fatal(err)
	}
	lease, err := f.repo.library.TryExclusive(context.Background(), "admin", "root", domain.NewID())
	if err != nil {
		t.Fatal("failed recovery leaked source lease", err)
	}
	lease.Close()
}

func TestSameSourceRecoveryBudgetSurvivesSeekAndRevokedAuth(t *testing.T) {
	f := recoverySetup(t, nil, 0)
	d := f.create(t, "same-source-create")
	for range 2 {
		var err error
		d, err = f.player.Control(context.Background(), f.who, d.SessionID, "recover", playback.ControlRequest{AttemptID: d.AttemptID, PositionMs: 2000, ObservedError: &playback.ObservedError{Code: "NETWORK"}}, domain.NewID())
		if err != nil {
			t.Fatal(err)
		}
		d, err = f.player.Control(context.Background(), f.who, d.SessionID, "seek", playback.ControlRequest{AttemptID: d.AttemptID, PositionMs: 3000}, domain.NewID())
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err := f.player.Control(context.Background(), f.who, d.SessionID, "recover", playback.ControlRequest{AttemptID: d.AttemptID, PositionMs: 2000, ObservedError: &playback.ObservedError{Code: "NETWORK"}}, "third-same-source")
	recoveryError(t, err, "RECOVERY_EXHAUSTED")
	if err = f.repo.DeleteAuth(context.Background(), f.who.ID); err != nil {
		t.Fatal(err)
	}
	_, err = f.player.RecoverSource(context.Background(), f.who, d.SessionID, recoveryRequest(d, f.tracks[1].ID, nil), "revoked-auth-request")
	recoveryError(t, err, "AUTH_REQUIRED")
}

func TestRecoveryRejectsChangedOriginalFileWithoutCreatingAnAsset(t *testing.T) {
	f := recoverySetup(t, nil, 0)
	e := f.evidence(t, 1)
	d := f.create(t, "changed-file-create")
	if err := os.WriteFile(filepath.Join(filepath.Dir(f.path), f.tracks[1].Path), []byte("changed target"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := f.player.RecoverSource(context.Background(), f.who, d.SessionID, recoveryRequest(d, f.tracks[1].ID, &e), "changed-source-request")
	recoveryError(t, err, "RESOURCE_CHANGED")
	current, err := f.repo.FindTrack(context.Background(), "admin", f.tracks[1].ID)
	if err != nil || current.AssetID != f.tracks[1].AssetID {
		t.Fatal("recovery fabricated an asset", err)
	}
}
