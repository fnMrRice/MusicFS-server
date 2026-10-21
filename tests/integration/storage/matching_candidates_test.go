package storage_test

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/metadata/identification/candidates"
	"github.com/fnMrRice/MusicFS-server/src/metadata/matching"
)

func TestMatchingCandidateRefreshIsStableAndManualDecisionsWin(t *testing.T) {
	s, _, tracks := matchingFixture(t, 3)
	ctx := context.Background()
	a, b, c := tracks[0].ID, tracks[1].ID, tracks[2].ID
	snapshot := matchingSnapshot(t, s, a, b, c)
	results := matchingResults(t, snapshot)
	if len(results) != 2 {
		t.Fatal("metadata candidate fixture failed")
	}
	first, err := s.SaveCandidates(ctx, "admin", snapshot, results)
	if err != nil || first.Revision != 0 || len(first.Items) != 2 {
		t.Fatalf("refresh failed: %+v %v", first, err)
	}
	retry, err := s.SaveCandidates(ctx, "admin", snapshot, results)
	if err != nil || !reflect.DeepEqual(retry, first) {
		t.Fatal("natural idempotency changed IDs or timestamps", err)
	}
	page, err := s.Candidates(ctx, "admin", a, "", 1)
	if err != nil || len(page.Items) != 1 || page.NextAfter == nil {
		t.Fatal("missing candidate page", err)
	}
	next, err := s.Candidates(ctx, "admin", a, *page.NextAfter, 1)
	if err != nil || len(next.Items) != 1 || next.NextAfter != nil || next.Items[0].ID == page.Items[0].ID {
		t.Fatal("candidate pagination repeated/lost results", err)
	}
	if _, err = s.SetExclusion(ctx, "admin", matching.ExclusionRequest{LeftSourceTrackID: a, RightSourceTrackID: b, Excluded: true}, "exclude-pair-01"); err != nil {
		t.Fatal(err)
	}
	_, err = s.SaveCandidates(ctx, "admin", snapshot, results)
	matchingError(t, err, "CONFLICT")
	snapshot = matchingSnapshot(t, s, a, b, c)
	results = matchingResults(t, snapshot)
	if len(results) != 1 {
		t.Fatal("excluded pair entered comparison")
	}
	if _, err = s.SaveCandidates(ctx, "admin", snapshot, results); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ManualLink(ctx, "admin", matching.LinkRequest{ExpectedRevision: 1, SourceTrackIDs: []string{a, c}}, "link-pair-001"); err != nil {
		t.Fatal(err)
	}
	page, err = s.Candidates(ctx, "admin", a, "", 20)
	if err != nil || len(page.Items) != 0 || page.Revision != 2 {
		t.Fatal("manual association left live candidates", err)
	}
	snapshot = matchingSnapshot(t, s, a, b, c)
	if len(matchingResults(t, snapshot)) != 0 {
		t.Fatal("manual and excluded pairs were proposed again")
	}
	var active int
	s.DB.QueryRow("SELECT COUNT(*) FROM matching_candidates WHERE status IN ('linked','excluded')").Scan(&active)
	if active < 2 {
		t.Fatal("candidate decision status history was not retained")
	}
}

func TestMatchingSnapshotRejectsAssetMetadataAndFingerprintChanges(t *testing.T) {
	t.Run("asset-and-metadata", func(t *testing.T) {
		s, items, tracks := matchingFixture(t, 2)
		ctx := context.Background()
		snapshot := matchingSnapshot(t, s, tracks[0].ID, tracks[1].ID)
		results := matchingResults(t, snapshot)
		if _, err := s.SaveCandidates(ctx, "admin", snapshot, results); err != nil {
			t.Fatal(err)
		}
		title := "Changed title"
		items[0].Title = &title
		publishEvidenceTracks(t, s, items...)
		_, err := s.SaveCandidates(ctx, "admin", snapshot, results)
		matchingError(t, err, "CONFLICT")
		page, err := s.Candidates(ctx, "admin", tracks[0].ID, "", 20)
		if err != nil || len(page.Items) != 0 {
			t.Fatal("stale metadata candidate remained visible", err)
		}
		snapshot = matchingSnapshot(t, s, tracks[0].ID, tracks[1].ID)
		items[0].ObservedVersion = "v2"
		publishEvidenceTracks(t, s, items...)
		_, err = s.SaveCandidates(ctx, "admin", snapshot, nil)
		matchingError(t, err, "CONFLICT")
	})
	t.Run("fingerprint-without-library-revision", func(t *testing.T) {
		s, _, tracks := matchingFixture(t, 2)
		ctx := context.Background()
		snapshot := matchingSnapshot(t, s, tracks[0].ID, tracks[1].ID)
		index := matchingFingerprint(t, s, fpTarget(tracks[0]))
		fresh := matchingSnapshot(t, s, tracks[0].ID, tracks[1].ID)
		if fresh.LibraryRevision != snapshot.LibraryRevision || fresh.Anchor.BindingRevision == snapshot.Anchor.BindingRevision {
			t.Fatal("fingerprint binding not independently versioned")
		}
		_, err := s.SaveCandidates(ctx, "admin", snapshot, matchingResults(t, snapshot))
		matchingError(t, err, "CONFLICT")
		if _, err = s.SaveCandidates(ctx, "admin", fresh, matchingResults(t, fresh)); err != nil {
			t.Fatal(err)
		}
		if err = s.InvalidateFingerprint(ctx, "admin", index.ID); err != nil {
			t.Fatal(err)
		}
		_, err = s.SaveCandidates(ctx, "admin", fresh, matchingResults(t, fresh))
		matchingError(t, err, "CONFLICT")
		page, err := s.Candidates(ctx, "admin", tracks[0].ID, "", 20)
		if err != nil || len(page.Items) != 0 {
			t.Fatal("invalidated fingerprint binding remained live", err)
		}
	})
}

func TestMatchingOwnerPoolProofBindingAndManualConflicts(t *testing.T) {
	s, items, tracks := matchingFixture(t, 3)
	ctx := context.Background()
	private := matchingPrivateTrack(t, s)
	_, err := s.CandidateSnapshot(ctx, "admin", private.ID, nil, 20)
	matchingError(t, err, "NOT_FOUND")
	_, err = s.CandidateSnapshot(ctx, "admin", tracks[0].ID, []string{private.ID}, 20)
	matchingError(t, err, "NOT_FOUND")
	_, err = s.CandidateSnapshot(ctx, "admin", tracks[0].ID, []string{tracks[1].ID, tracks[1].ID}, 20)
	matchingError(t, err, "INVALID_ARGUMENT")
	_, err = s.CandidateSnapshot(ctx, "admin", tracks[0].ID, []string{tracks[0].ID}, 20)
	matchingError(t, err, "INVALID_ARGUMENT")
	claim := strings.Repeat("a", 128)
	items[0].ProviderSHA512 = &claim
	items[1].ProviderSHA512 = &claim
	tracks = publishEvidenceTracks(t, s, items...)
	snapshot := matchingSnapshot(t, s, tracks[0].ID)
	if len(snapshot.Sources) != 2 || snapshot.Anchor.Input.FileHash != nil {
		t.Fatal("private source or provider claim entered trusted pool")
	}
	for _, source := range snapshot.Sources {
		if source.Input.FileHash != nil {
			t.Fatal("provider hash became verified")
		}
	}
	for _, track := range []int{0, 2} {
		ticket, err := s.BeginEvidence(ctx, "admin", hashBinding(tracks[track].Asset))
		if err != nil {
			t.Fatal(err)
		}
		if err = s.PublishEvidence(ctx, "admin", ticket, hashResult()); err != nil {
			t.Fatal(err)
		}
	}
	priority, err := s.CandidateSnapshot(ctx, "admin", tracks[0].ID, nil, 1)
	if err != nil || len(priority.Sources) != 1 || priority.Sources[0].Input.Asset.SourceTrackID != tracks[2].ID || priority.Anchor.Input.FileHash.FileSizeBytes != 1000 {
		t.Fatal("verified equality did not lead bounded pool", err)
	}
	result := matchingResults(t, priority)
	if len(result) != 1 || result[0].Score != 100 {
		t.Fatal("verified bytes lacked strong candidate")
	}
	forged := append([]candidates.Candidate(nil), result...)
	forged[0].Reasons = append([]candidates.Reason(nil), result[0].Reasons...)
	forged[0].Reasons[0].LeftEvidenceID = domain.NewID()
	_, err = s.SaveCandidates(ctx, "admin", priority, forged)
	matchingError(t, err, "INVALID_ARGUMENT")
	_, err = s.SaveCandidates(ctx, "bob", priority, result)
	matchingError(t, err, "INVALID_ARGUMENT")
	first, err := s.ManualLink(ctx, "admin", matching.LinkRequest{SourceTrackIDs: []string{tracks[0].ID}}, "separate-one-01")
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.ManualLink(ctx, "admin", matching.LinkRequest{ExpectedRevision: first.Revision, SourceTrackIDs: []string{tracks[2].ID}}, "separate-two-01")
	if err != nil {
		t.Fatal(err)
	}
	priority = matchingSnapshot(t, s, tracks[0].ID, tracks[2].ID)
	if len(priority.Blocked) != 1 || len(matchingResults(t, priority)) != 0 {
		t.Fatal("different manual recordings were silently proposed to merge")
	}
	_, err = s.Candidates(ctx, "bob", tracks[0].ID, "", 20)
	matchingError(t, err, "NOT_FOUND")
}

func TestMatchingSnapshotFingerprintSelectionIsBounded(t *testing.T) {
	s, _, tracks := matchingFixture(t, 2)
	for i := 0; i < 9; i++ {
		target := fpTarget(tracks[0])
		target.StartMs += int64(i)
		matchingFingerprint(t, s, target)
	}
	snapshot := matchingSnapshot(t, s, tracks[0].ID, tracks[1].ID)
	if len(snapshot.Anchor.Fingerprints) != 8 {
		t.Fatal("comparison detail selection is not bounded")
	}
	target := fpTarget(tracks[0])
	target.StartMs += 10
	matchingFingerprint(t, s, target)
	_, err := s.SaveCandidates(context.Background(), "admin", snapshot, matchingResults(t, snapshot))
	matchingError(t, err, "CONFLICT")
}
