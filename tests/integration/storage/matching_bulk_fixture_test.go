package storage_test

import (
	"fmt"
	"sort"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/metadata/matching"
)

func bulkMergeFixture(t *testing.T, count int, configure func(int, *library.Observation)) (*sqlite.Store, []library.Observation, []library.Track) {
	t.Helper()
	s := evidenceStore(t)
	items := make([]library.Observation, count)
	for i := range items {
		items[i] = evidenceObservation(fmt.Sprintf("bulk-%04d", i), "v1")
		title, duration := fmt.Sprintf("Song %04d", i/2), int64(180000)
		items[i].Title, items[i].Artists, items[i].DurationMs = &title, []string{"Test Artist"}, &duration
		if configure != nil {
			configure(i, &items[i])
		}
	}
	publishEvidenceTracks(t, s, items...)
	tracks := []library.Track{}
	after := ""
	for {
		page, err := s.ListTracks(t.Context(), "admin", "local", after, 200)
		if err != nil {
			t.Fatal(err)
		}
		tracks = append(tracks, page.Items...)
		if page.NextCursor == nil {
			break
		}
		after = *page.NextCursor
	}
	sort.Slice(tracks, func(i, j int) bool { return tracks[i].DisplayName < tracks[j].DisplayName })
	return s, items, tracks
}

func saveBulkMergePair(t *testing.T, s *sqlite.Store, left, right string) {
	t.Helper()
	snapshot := matchingSnapshot(t, s, left, right)
	snapshot.Partial = true
	results := matchingResults(t, snapshot)
	if len(results) != 1 {
		t.Fatal("expected one valid candidate")
	}
	if _, err := s.SaveCandidates(t.Context(), "admin", snapshot, results); err != nil {
		t.Fatal(err)
	}
}

func bulkScoreRequest(revision int64, score int) matching.BulkMergeRequest {
	return matching.BulkMergeRequest{ExpectedRevision: revision, Mode: "score", MinimumScore: &score}
}
