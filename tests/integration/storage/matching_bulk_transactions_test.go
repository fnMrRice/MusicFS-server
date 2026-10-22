package storage_test

import (
	"sync"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/metadata/matching"
)

func TestMatchingBulkMergeFailureRollsBackEveryGroupAndReceipt(t *testing.T) {
	s, _, tracks := bulkMergeFixture(t, 4, nil)
	ctx := t.Context()
	if _, err := s.DB.Exec(`CREATE TEMP TRIGGER fail_bulk_audit BEFORE INSERT ON link_changes
 WHEN NEW.sequence=3 BEGIN SELECT RAISE(ABORT,'synthetic audit failure'); END`); err != nil {
		t.Fatal(err)
	}
	request := matching.BulkMergeRequest{Mode: "title_artist"}
	if _, err := s.BulkMerge(ctx, "admin", request, "bulk-atomic-failure"); err == nil {
		t.Fatal("injected audit failure did not abort the operation")
	}
	for _, track := range tracks {
		link, err := s.GetLink(ctx, "admin", track.ID)
		if err != nil || link.RecordingID != nil || link.Revision != 0 {
			t.Fatal("failed bulk transaction retained a source move", err)
		}
	}
	for _, table := range []string{"recordings", "link_changes", "idempotency", "timeline_evidence"} {
		var count int
		query := "SELECT COUNT(*) FROM " + table
		if table == "idempotency" {
			query += " WHERE route='matching/bulk-merge'"
		}
		if err := s.DB.QueryRow(query).Scan(&count); err != nil || count != 0 {
			t.Fatalf("failed transaction retained %s entries: %d %v", table, count, err)
		}
	}
	if _, err := s.DB.Exec("DROP TRIGGER fail_bulk_audit"); err != nil {
		t.Fatal(err)
	}
	result, err := s.BulkMerge(ctx, "admin", request, "bulk-atomic-failure")
	if err != nil || result.Revision != 1 || result.MergedGroups != 2 || result.MergedSources != 4 {
		t.Fatalf("failed request could not be retried: %+v %v", result, err)
	}
	var timelines int
	if err := s.DB.QueryRow("SELECT COUNT(*) FROM timeline_evidence").Scan(&timelines); err != nil || timelines != 0 {
		t.Fatal("bulk merge created timeline evidence", err)
	}
}

func TestMatchingBulkMergeConcurrentRevisionHasSingleWinner(t *testing.T) {
	s, _, _ := bulkMergeFixture(t, 4, nil)
	ctx := t.Context()
	request := matching.BulkMergeRequest{Mode: "title_artist"}
	var wg sync.WaitGroup
	errors := make([]error, 2)
	results := make([]matching.BulkMergeResult, 2)
	for i, key := range []string{"bulk-race-first", "bulk-race-second"} {
		wg.Add(1)
		go func(i int, key string) {
			defer wg.Done()
			results[i], errors[i] = s.BulkMerge(ctx, "admin", request, key)
		}(i, key)
	}
	wg.Wait()
	winner, loser := 0, 1
	if errors[winner] != nil {
		winner, loser = loser, winner
	}
	if errors[winner] != nil || results[winner].Revision != 1 || results[winner].MergedGroups != 2 {
		t.Fatalf("no successful bulk operation: %v", errors)
	}
	matchingError(t, errors[loser], "CONFLICT")
	changes, err := s.ChangesAfter(ctx, "admin", 0, 50)
	if err != nil || len(changes.Items) != 4 || changes.Revision != 1 {
		t.Fatal("concurrent bulk operations both committed", err)
	}
}

func TestMatchingBulkMergeRetainsUnavailableExistingMembers(t *testing.T) {
	for _, mode := range []string{"score", "title_artist"} {
		t.Run(mode, func(t *testing.T) {
			s, items, tracks := bulkMergeFixture(t, 3, nil)
			ctx := t.Context()
			old, err := s.ManualLink(ctx, "admin", matching.LinkRequest{SourceTrackIDs: []string{tracks[0].ID, tracks[2].ID}}, "bulk-unavailable-old-group")
			if err != nil {
				t.Fatal(err)
			}
			publishEvidenceTracks(t, s, items[:2]...)
			saveBulkMergePair(t, s, tracks[0].ID, tracks[1].ID)
			request := matching.BulkMergeRequest{ExpectedRevision: 1, Mode: mode}
			if mode == "score" {
				request = bulkScoreRequest(1, 35)
			}
			result, err := s.BulkMerge(ctx, "admin", request, "bulk-keep-unavailable-member")
			if err != nil || result.MergedGroups != 1 || result.MergedSources != 3 || result.SkippedGroups != 0 {
				t.Fatalf("old unavailable member was dropped: %+v %v", result, err)
			}
			recording, err := s.Recording(ctx, "admin", *old.RecordingID)
			if err != nil || len(recording.SourceTrackIDs) != 3 {
				t.Fatal("original recording was split", err)
			}
		})
	}
}
