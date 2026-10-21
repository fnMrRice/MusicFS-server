package storage_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/metadata/identification"
	"github.com/fnMrRice/MusicFS-server/src/metadata/identification/candidates"
	"github.com/fnMrRice/MusicFS-server/src/metadata/matching"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func matchingFixture(t *testing.T, count int) (*sqlite.Store, []library.Observation, []library.Track) {
	t.Helper()
	s := evidenceStore(t)
	items := make([]library.Observation, count)
	for i := range items {
		items[i] = evidenceObservation(string(rune('a'+i)), "v1")
		title, duration := "Example Song", int64(180000)
		items[i].Title, items[i].DurationMs, items[i].Artists = &title, &duration, []string{"Test Artist"}
	}
	return s, items, publishEvidenceTracks(t, s, items...)
}

func matchingError(t *testing.T, err error, code string) {
	t.Helper()
	var e *domain.Error
	if !errors.As(err, &e) || e.Code != code {
		t.Fatalf("expected %s, received %v", code, err)
	}
}

func matchingResults(t *testing.T, snapshot matching.Snapshot) []candidates.Candidate {
	t.Helper()
	inputs := make([]candidates.Input, len(snapshot.Sources))
	for i, s := range snapshot.Sources {
		inputs[i] = s.Input
	}
	result, err := candidates.Find(context.Background(), snapshot.Anchor.Input, inputs, snapshot.Blocked)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func matchingSnapshot(t *testing.T, s *sqlite.Store, anchor string, ids ...string) matching.Snapshot {
	t.Helper()
	snapshot, err := s.CandidateSnapshot(context.Background(), "admin", anchor, ids, 256)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func matchingPrivateTrack(t *testing.T, s *sqlite.Store) library.Track {
	t.Helper()
	ctx := context.Background()
	if _, _, err := s.Enqueue(ctx, "bob", "private", testkit.ScanPlan("music"), domain.NewID()); err != nil {
		t.Fatal(err)
	}
	j, err := s.Claim(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Publish(ctx, j, testkit.ScanPublication(j, []library.Observation{evidenceObservation("private", "v1")})); err != nil {
		t.Fatal(err)
	}
	if err = s.Finish(ctx, j, 1, nil); err != nil {
		t.Fatal(err)
	}
	page, err := s.ListTracks(ctx, "bob", "private", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	return page.Items[0]
}

// Exercise the actual journal/index transaction. File validation belongs to the
// real-file publication tests; this helper supplies its validated object result.
func matchingFingerprint(t *testing.T, s *sqlite.Store, target identification.FingerprintTarget) identification.FingerprintIndex {
	t.Helper()
	ctx := context.Background()
	j := fpClaim(t, s, target)
	p, raw := fpPublication(t, j)
	if err := s.BeginPublication(ctx, p); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	p.Object.Size, p.Object.SHA256 = int64(len(raw)), hex.EncodeToString(sum[:])
	if err := s.WrittenPublication(ctx, p.Owner, p.ID, p.Object); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPublicationStage(ctx, p.Owner, p.ID, "written", "published"); err != nil {
		t.Fatal(err)
	}
	if err := s.ReferencePublication(ctx, p.Owner, p.ID); err != nil {
		t.Fatal(err)
	}
	job, err := s.GetFingerprintJob(ctx, p.Owner, j.ID)
	if err != nil || job.Result == nil {
		t.Fatalf("missing published index: %v", err)
	}
	return *job.Result
}
