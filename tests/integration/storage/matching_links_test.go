package storage_test

import (
	"context"
	"reflect"
	"sync"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/metadata/matching"
)

func TestMatchingLinkSplitUnlinkPreservesSourcesAndAudit(t *testing.T) {
	s, _, tracks := matchingFixture(t, 3)
	ctx := context.Background()
	ids := []string{tracks[0].ID, tracks[1].ID, tracks[2].ID}
	var libraryRevision int64
	if err := s.DB.QueryRow("SELECT revision FROM library_revision").Scan(&libraryRevision); err != nil {
		t.Fatal(err)
	}
	linked, err := s.ManualLink(ctx, "admin", matching.LinkRequest{SourceTrackIDs: ids}, "link-all-001")
	if err != nil || linked.Revision != 1 || linked.RecordingID == nil || len(linked.Changes) != 3 {
		t.Fatalf("link failed %+v %v", linked, err)
	}
	original := *linked.RecordingID
	split, err := s.Split(ctx, "admin", matching.SplitRequest{ExpectedRevision: 1, RecordingID: original, SourceTrackIDs: ids[1:]}, "split-pair-01")
	if err != nil || split.Revision != 2 || split.RecordingID == nil || *split.RecordingID == original {
		t.Fatalf("split failed: %+v %v", split, err)
	}
	r, err := s.Recording(ctx, "admin", original)
	if err != nil || !reflect.DeepEqual(r.SourceTrackIDs, ids[:1]) || r.Revision != 2 {
		t.Fatalf("original membership changed incorrectly: %+v %v", r, err)
	}
	unlinked, err := s.Unlink(ctx, "admin", matching.UnlinkRequest{ExpectedRevision: 2, SourceTrackID: ids[2]}, "unlink-one-01")
	if err != nil || unlinked.Revision != 3 {
		t.Fatal(err)
	}
	link, err := s.GetLink(ctx, "admin", ids[2])
	if err != nil || link.RecordingID != nil || link.Decision != "unlinked" || link.Revision != 3 {
		t.Fatalf("unlink decision lost: %+v %v", link, err)
	}
	for _, old := range tracks {
		fresh, err := s.FindTrack(ctx, "admin", old.ID)
		if err != nil || fresh.AssetID != old.AssetID || !reflect.DeepEqual(fresh.Asset, old.Asset) {
			t.Fatalf("manual operation changed a source or evidence: %v", err)
		}
	}
	var unchanged int64
	s.DB.QueryRow("SELECT revision FROM library_revision").Scan(&unchanged)
	if unchanged != libraryRevision {
		t.Fatal("manual links rewrote the library")
	}
	page, err := s.ChangesAfter(ctx, "admin", 0, 4)
	if err != nil || len(page.Items) != 4 || page.NextAfter == nil || page.Revision != 3 {
		t.Fatalf("bad audit page: %+v %v", page, err)
	}
	tail, err := s.ChangesAfter(ctx, "admin", *page.NextAfter, 4)
	if err != nil || len(tail.Items) != 2 || tail.NextAfter != nil {
		t.Fatal("audit pagination lost changes", err)
	}
	changes := append(page.Items, tail.Items...)
	for i, c := range changes {
		if c.Sequence != int64(i+1) || c.Owner != "admin" || c.SourceTrackID == nil || c.CreatedAt.Location().String() != "UTC" {
			t.Fatalf("invalid audit entry: %+v", c)
		}
	}
	if *changes[3].FromRecordingID != original || *changes[3].ToRecordingID != *split.RecordingID || changes[5].ToRecordingID != nil || *changes[5].FromRecordingID != *split.RecordingID {
		t.Fatal("original/reversible targets were lost")
	}
}

func TestMatchingExclusionsAndFailedOperationsAreAtomic(t *testing.T) {
	s, _, tracks := matchingFixture(t, 3)
	ctx := context.Background()
	a, b, c := tracks[0].ID, tracks[1].ID, tracks[2].ID
	exclude := matching.ExclusionRequest{LeftSourceTrackID: a, RightSourceTrackID: b, Excluded: true}
	if _, err := s.SetExclusion(ctx, "admin", exclude, "exclude-ab-01"); err != nil {
		t.Fatal(err)
	}
	_, err := s.ManualLink(ctx, "admin", matching.LinkRequest{ExpectedRevision: 1, SourceTrackIDs: []string{a, b}}, "blocked-ab-01")
	matchingError(t, err, "CONFLICT")
	linked, err := s.ManualLink(ctx, "admin", matching.LinkRequest{ExpectedRevision: 1, SourceTrackIDs: []string{a, c}}, "allowed-ac-01")
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.ManualLink(ctx, "admin", matching.LinkRequest{ExpectedRevision: 2, SourceTrackIDs: []string{b}, TargetRecordingID: linked.RecordingID}, "target-b-001")
	matchingError(t, err, "CONFLICT")
	if rev, _ := s.MatchingRevision(ctx, "admin"); rev != 2 {
		t.Fatal("failed operation advanced revision")
	}
	var recordings int
	s.DB.QueryRow("SELECT COUNT(*) FROM recordings").Scan(&recordings)
	if recordings != 1 {
		t.Fatal("failed operation leaked a recording")
	}
	exclude.ExpectedRevision, exclude.Excluded = 2, false
	if _, err = s.SetExclusion(ctx, "admin", exclude, "allow-ab-001"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ManualLink(ctx, "admin", matching.LinkRequest{ExpectedRevision: 3, SourceTrackIDs: []string{b}, TargetRecordingID: linked.RecordingID}, "target-b-002"); err != nil {
		t.Fatal(err)
	}
	exclude.ExpectedRevision, exclude.Excluded = 4, true
	_, err = s.SetExclusion(ctx, "admin", exclude, "exclude-ab-02")
	matchingError(t, err, "CONFLICT")
	_, err = s.Split(ctx, "admin", matching.SplitRequest{ExpectedRevision: 4, RecordingID: *linked.RecordingID, SourceTrackIDs: []string{a, b, c}}, "split-all-001")
	matchingError(t, err, "INVALID_ARGUMENT")
}

func TestMatchingRevisionRaceIdempotencyAndOwnerIsolation(t *testing.T) {
	s, _, tracks := matchingFixture(t, 3)
	ctx := context.Background()
	private := matchingPrivateTrack(t, s)
	requests := []matching.LinkRequest{{SourceTrackIDs: []string{tracks[0].ID, tracks[1].ID}}, {SourceTrackIDs: []string{tracks[2].ID}}}
	var wg sync.WaitGroup
	errs := make([]error, 2)
	results := make([]matching.Mutation, 2)
	for i := range requests {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = s.ManualLink(ctx, "admin", requests[i], "race-key-"+string(rune('a'+i)))
		}(i)
	}
	wg.Wait()
	winner, loser := 0, 1
	if errs[0] != nil {
		winner, loser = 1, 0
	}
	if errs[winner] != nil {
		t.Fatalf("both decisions failed: %v", errs)
	}
	matchingError(t, errs[loser], "CONFLICT")
	replay, err := s.ManualLink(ctx, "admin", requests[winner], "race-key-"+string(rune('a'+winner)))
	if err != nil || !reflect.DeepEqual(replay, results[winner]) {
		t.Fatal("exact retry changed result", err)
	}
	changed := requests[winner]
	changed.ExpectedRevision = 1
	_, err = s.ManualLink(ctx, "admin", changed, "race-key-"+string(rune('a'+winner)))
	matchingError(t, err, "CONFLICT")
	_, err = s.ManualLink(ctx, "admin", matching.LinkRequest{ExpectedRevision: 1, SourceTrackIDs: []string{private.ID}}, domain.NewID())
	matchingError(t, err, "NOT_FOUND")
	bob, err := s.ManualLink(ctx, "bob", matching.LinkRequest{SourceTrackIDs: []string{private.ID}}, "private-link-01")
	if err != nil || bob.Changes[0].Sequence != 1 {
		t.Fatal("audit exposed another owner's sequence", err)
	}
	_, err = s.Recording(ctx, "admin", *bob.RecordingID)
	matchingError(t, err, "NOT_FOUND")
	_, err = s.GetLink(ctx, "admin", private.ID)
	matchingError(t, err, "NOT_FOUND")
	_, err = s.ManualLink(ctx, "admin", matching.LinkRequest{ExpectedRevision: 1, SourceTrackIDs: []string{tracks[0].ID}, TargetRecordingID: bob.RecordingID}, domain.NewID())
	matchingError(t, err, "NOT_FOUND")
	page, err := s.ChangesAfter(ctx, "bob", 0, 100)
	if err != nil || len(page.Items) != 1 || page.Revision != 1 {
		t.Fatal("audit owner isolation failed", err)
	}
	if rev, _ := s.MatchingRevision(ctx, "admin"); rev != 1 {
		t.Fatal("unauthorized operation changed state")
	}
}
