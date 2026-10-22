package storage_test

import (
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/metadata/matching"
)

func TestMatchingBulkMergeScoreChecksExpandedTransitiveExclusions(t *testing.T) {
	for _, oldMember := range []bool{false, true} {
		name := "transitive-pair"
		if oldMember {
			name = "expanded-old-member"
		}
		t.Run(name, func(t *testing.T) {
			s, _, tracks := bulkMergeFixture(t, 6, func(i int, item *library.Observation) {
				if i < 4 {
					title := "Same Song"
					item.Title = &title
				}
			})
			ctx := t.Context()
			var revision int64
			left := tracks[0].ID
			if oldMember {
				if _, err := s.ManualLink(ctx, "admin", matching.LinkRequest{SourceTrackIDs: []string{tracks[0].ID, tracks[3].ID}}, "bulk-old-excluded"); err != nil {
					t.Fatal(err)
				}
				left, revision = tracks[3].ID, 1
			}
			if _, err := s.SetExclusion(ctx, "admin", matching.ExclusionRequest{ExpectedRevision: revision, LeftSourceTrackID: left, RightSourceTrackID: tracks[2].ID, Excluded: true}, "bulk-transitive-exclude"); err != nil {
				t.Fatal(err)
			}
			revision++
			saveBulkMergePair(t, s, tracks[0].ID, tracks[1].ID)
			saveBulkMergePair(t, s, tracks[1].ID, tracks[2].ID)
			saveBulkMergePair(t, s, tracks[4].ID, tracks[5].ID)
			result, err := s.BulkMerge(ctx, "admin", bulkScoreRequest(revision, 35), "bulk-transitive-merge")
			if err != nil || result.MergedGroups != 1 || result.MergedSources != 2 || result.SkippedGroups != 1 || result.Revision != revision+1 {
				t.Fatalf("transitive exclusion was bypassed: %+v %v", result, err)
			}
			for _, i := range []int{1, 2} {
				link, err := s.GetLink(ctx, "admin", tracks[i].ID)
				if err != nil || link.RecordingID != nil {
					t.Fatal("blocked group partially committed", err)
				}
			}
		})
	}
}

func TestMatchingBulkMergeScoreBridgesCompleteExistingGroups(t *testing.T) {
	s, _, tracks := bulkMergeFixture(t, 5, func(_ int, item *library.Observation) {
		title := "Bridge Song"
		item.Title = &title
	})
	ctx := t.Context()
	if _, err := s.ManualLink(ctx, "admin", matching.LinkRequest{SourceTrackIDs: []string{tracks[0].ID, tracks[3].ID}}, "bulk-bridge-first"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ManualLink(ctx, "admin", matching.LinkRequest{ExpectedRevision: 1, SourceTrackIDs: []string{tracks[2].ID, tracks[4].ID}}, "bulk-bridge-second"); err != nil {
		t.Fatal(err)
	}
	saveBulkMergePair(t, s, tracks[0].ID, tracks[1].ID)
	saveBulkMergePair(t, s, tracks[1].ID, tracks[2].ID)
	result, err := s.BulkMerge(ctx, "admin", bulkScoreRequest(2, 35), "bulk-bridge-all")
	if err != nil || result.MergedGroups != 1 || result.MergedSources != 5 || result.SkippedGroups != 0 {
		t.Fatalf("valid bridge lost existing groups: %+v %v", result, err)
	}
}

func TestMatchingBulkMergeIgnoresUnlinkedUnavailableAndStaleSources(t *testing.T) {
	for _, mode := range []string{"score", "title_artist"} {
		t.Run(mode, func(t *testing.T) {
			s, items, tracks := bulkMergeFixture(t, 6, nil)
			ctx := t.Context()
			if _, err := s.Unlink(ctx, "admin", matching.UnlinkRequest{SourceTrackID: tracks[0].ID}, "bulk-explicit-unlink"); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 6; i += 2 {
				saveBulkMergePair(t, s, tracks[i].ID, tracks[i+1].ID)
			}
			if mode == "score" {
				title := "Changed metadata"
				items[2].Title = &title
			} else {
				items = append(items[:2], items[3:]...)
			}
			publishEvidenceTracks(t, s, items...)
			request := matching.BulkMergeRequest{ExpectedRevision: 1, Mode: mode}
			if mode == "score" {
				request = bulkScoreRequest(1, 35)
			}
			result, err := s.BulkMerge(ctx, "admin", request, "bulk-current-only")
			if err != nil || result.MergedGroups != 1 || result.MergedSources != 2 || result.Revision != 2 {
				t.Fatalf("stale or manually unlinked source merged: %+v %v", result, err)
			}
			link, err := s.GetLink(ctx, "admin", tracks[0].ID)
			if err != nil || link.Decision != "unlinked" || link.RecordingID != nil {
				t.Fatal("manual unlink was overwritten", err)
			}
		})
	}
}

func TestMatchingBulkMergeWholeLibraryAndOversizeGroups(t *testing.T) {
	t.Run("candidate-pages-and-source-limit", func(t *testing.T) {
		s, _, tracks := bulkMergeFixture(t, 262, nil)
		for i := 0; i < len(tracks); i += 2 {
			saveBulkMergePair(t, s, tracks[i].ID, tracks[i+1].ID)
		}
		empty, err := s.BulkMerge(t.Context(), "admin", bulkScoreRequest(0, 36), "bulk-over-threshold")
		if err != nil || empty.MergedGroups != 0 || empty.Revision != 0 {
			t.Fatal("score threshold ignored", err)
		}
		result, err := s.BulkMerge(t.Context(), "admin", bulkScoreRequest(0, 35), "bulk-all-candidate-pages")
		if err != nil || result.MergedGroups != 131 || result.MergedSources != 262 || len(result.Changes) != 262 || result.Revision != 1 {
			t.Fatalf("whole-library operation was truncated: %+v %v", result, err)
		}
	})
	t.Run("oversized-group-skipped-intact", func(t *testing.T) {
		s, _, tracks := bulkMergeFixture(t, 260, func(i int, item *library.Observation) {
			if i < 258 {
				title := "Oversized Group"
				item.Title = &title
			}
		})
		result, err := s.BulkMerge(t.Context(), "admin", matching.BulkMergeRequest{Mode: "title_artist"}, "bulk-oversized-group")
		if err != nil || result.MergedGroups != 1 || result.MergedSources != 2 || result.SkippedGroups != 1 {
			t.Fatalf("oversized component was partially merged: %+v %v", result, err)
		}
		for _, i := range []int{0, 256, 257} {
			link, err := s.GetLink(t.Context(), "admin", tracks[i].ID)
			if err != nil || link.RecordingID != nil {
				t.Fatal("oversized group was truncated", err)
			}
		}
	})
}
