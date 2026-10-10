package playback_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/playback"
	"github.com/fnMrRice/MusicFS-server/src/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/sourceaccess"
	"github.com/fnMrRice/MusicFS-server/src/timeline"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestRecoveryCommitRechecksEvidenceAuthAndOldAttemptAtomically(t *testing.T) {
	for _, change := range []string{"revoke", "auth", "attempt", "asset"} {
		t.Run(change, func(t *testing.T) {
			f := recoverySetup(t, nil, 0)
			e := f.evidence(t, 1)
			d := f.create(t, "atomic-recovery-create")
			ctx := context.Background()
			check := timeline.Check{From: e.From, To: e.To, PositionMs: 4000, Profile: e.Profile, EvidenceID: e.ID}
			if _, err := f.repo.CheckTimeline(ctx, "admin", check); err != nil {
				t.Fatal(err)
			}
			want := "RESTART_REQUIRED"
			switch change {
			case "revoke":
				if _, err := f.repo.RevokeTimeline(ctx, "admin", e.ID, 1, "late-revocation"); err != nil {
					t.Fatal(err)
				}
			case "auth":
				if err := f.repo.DeleteAuth(ctx, f.who.ID); err != nil {
					t.Fatal(err)
				}
				want = "AUTH_REQUIRED"
			case "attempt":
				if _, err := f.player.Control(ctx, f.who, d.SessionID, "seek", playback.ControlRequest{AttemptID: d.AttemptID, PositionMs: 2000}, "late-attempt-change"); err != nil {
					t.Fatal(err)
				}
				want = "CONFLICT"
			case "asset":
				rescanRecoveryTarget(t, f)
				want = "RESOURCE_CHANGED"
			}
			candidate := d
			candidate.AttemptID = domain.NewID()
			candidate.AssetID = e.To.AssetID
			candidate.PositionOriginMs = 5000
			raw, _ := json.Marshal(candidate)
			err := f.repo.CommitRecovery(ctx, timeline.Commit{SessionID: d.SessionID, AuthID: f.who.ID, Owner: "admin", PreviousAttemptID: d.AttemptID, Check: check, Continue: true, Description: raw, Result: raw, ExpiresAt: d.ExpiresAt, Route: "late-commit-test", Key: "late-commit-key", Digest: "test"})
			recoveryError(t, err, want)
			var count int
			if err = f.repo.DB.QueryRow("SELECT COUNT(*) FROM idempotency WHERE route='late-commit-test'").Scan(&count); err != nil || count != 0 {
				t.Fatal("failed atomic recovery persisted a result", err)
			}
		})
	}
}

func rescanRecoveryTarget(t *testing.T, f recoveryFixture) {
	t.Helper()
	root := filepath.Dir(f.path)
	original, err := os.ReadFile(f.path)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, f.tracks[1].Path), shiftWave(t, original, 1500), 0600); err != nil {
		t.Fatal(err)
	}
	access, err := sourceaccess.New([]sourceaccess.RootConfig{{ID: "root", Path: root}})
	if err != nil {
		t.Fatal(err)
	}
	observations := []library.Observation{}
	for _, track := range f.tracks {
		entry, err := access.Inspect("root", track.Path)
		if err != nil {
			t.Fatal(err)
		}
		observations = append(observations, library.Observation{Observation: pluginproto.Observation{ExternalID: track.ExternalID, RootID: "root", RelativePath: track.Path, ObservedVersion: entry.ObservedVersion, DisplayName: track.DisplayName, MIME: entry.MIME, DurationMs: entry.DurationMs, SizeBytes: entry.SizeBytes}, Artists: []string{}})
	}
	ctx := context.Background()
	if _, _, err = f.repo.Enqueue(ctx, "admin", "root", testkit.ScanPlan("root"), "rescan-recovery-target"); err != nil {
		t.Fatal(err)
	}
	j, err := f.repo.Claim(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.repo.Publish(ctx, j, testkit.ScanPublication(j, observations)); err != nil {
		t.Fatal(err)
	}
	if err = f.repo.Finish(ctx, j, len(observations), nil); err != nil {
		t.Fatal(err)
	}
	current, err := f.repo.FindTrack(ctx, "admin", f.tracks[1].ID)
	if err != nil || current.AssetID == f.tracks[1].AssetID {
		t.Fatal("actual rescan did not create the changed asset", err)
	}
}
