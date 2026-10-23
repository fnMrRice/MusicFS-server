package storage_test

import (
	"reflect"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/metadata/matching"
)

func TestMatchingBulkMergeTitleArtistPreservesGroupsAndReplays(t *testing.T) {
	s, _, tracks := bulkMergeFixture(t, 6, func(i int, item *library.Observation) {
		if i < 4 {
			title := "Same Song"
			item.Title = &title
		}
	})
	ctx := t.Context()
	first, err := s.ManualLink(ctx, "admin", matching.LinkRequest{SourceTrackIDs: []string{tracks[0].ID, tracks[4].ID}}, "bulk-existing-first")
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.ManualLink(ctx, "admin", matching.LinkRequest{ExpectedRevision: 1, SourceTrackIDs: []string{tracks[1].ID, tracks[5].ID}}, "bulk-existing-second")
	if err != nil {
		t.Fatal(err)
	}
	private := matchingPrivateTrack(t, s)
	var libraryRevision int64
	if err = s.DB.QueryRow("SELECT revision FROM library_revision").Scan(&libraryRevision); err != nil {
		t.Fatal(err)
	}
	request := matching.BulkMergeRequest{ExpectedRevision: 2, Mode: "title_artist"}
	result, err := s.BulkMerge(ctx, "admin", request, "bulk-title-existing")
	if err != nil || result.Revision != 3 || result.MergedGroups != 1 || result.MergedSources != 6 || result.SkippedGroups != 0 || len(result.Changes) != 6 {
		t.Fatalf("unexpected bulk result: %+v %v", result, err)
	}
	target := result.Changes[0].ToRecordingID
	if target == nil || (*target != *first.RecordingID && *target != *second.RecordingID) {
		t.Fatal("existing recording was not reused")
	}
	recording, err := s.Recording(ctx, "admin", *target)
	if err != nil || len(recording.SourceTrackIDs) != 6 {
		t.Fatal("existing recording members were split", err)
	}
	for _, track := range tracks {
		fresh, err := s.FindTrack(ctx, "admin", track.ID)
		if err != nil || fresh.AssetID != track.AssetID || !reflect.DeepEqual(fresh.Asset, track.Asset) {
			t.Fatal("bulk merge modified source evidence", err)
		}
	}
	var unchanged int64
	if err = s.DB.QueryRow("SELECT revision FROM library_revision").Scan(&unchanged); err != nil || unchanged != libraryRevision {
		t.Fatal("bulk merge rewrote the library", err)
	}
	privateLink, err := s.GetLink(ctx, "bob", private.ID)
	if err != nil || privateLink.RecordingID != nil || privateLink.Revision != 0 {
		t.Fatal("bulk merge changed another owner", err)
	}
	if _, err = s.Unlink(ctx, "admin", matching.UnlinkRequest{ExpectedRevision: 3, SourceTrackID: tracks[0].ID}, "bulk-following-unlink"); err != nil {
		t.Fatal(err)
	}
	replay, err := s.BulkMerge(ctx, "admin", request, "bulk-title-existing")
	if err != nil || !reflect.DeepEqual(replay, result) {
		t.Fatal("bulk replay did not retain original counters and audit", err)
	}
	_, err = s.BulkMerge(ctx, "admin", request, "bulk-stale-revision")
	matchingError(t, err, "CONFLICT")
	_, err = s.BulkMerge(ctx, "admin", bulkScoreRequest(2, 35), "bulk-title-existing")
	matchingError(t, err, "CONFLICT")
}

func TestMatchingBulkMergeTitleArtistUsesCompleteLabels(t *testing.T) {
	s, _, tracks := bulkMergeFixture(t, 9, func(i int, item *library.Observation) {
		title := "Song Name"
		item.Title, item.Artists = &title, []string{"Artist A", "Artist B"}
		switch i {
		case 0:
			title = "  SONG\u3000Name "
			item.Artists = []string{" ARTIST A ", "Artist B", "Artist A"}
		case 1:
			item.Artists = []string{"artist b", "artist a"}
			duration := int64(300000)
			item.DurationMs = &duration
		case 2:
			item.Artists = []string{"Artist A"}
		case 3:
			// 入库先拆分并丢弃空项，清理后的完整署名集合应参与匹配。
			item.Artists = []string{"Artist A / Artist B", " / "}
		case 4:
			title = "Song Name (Live)"
		case 5:
			item.Title = nil
		case 6:
			item.Artists = nil
		case 7:
			title = " \u3000 "
		case 8:
			item.Artists = []string{" / ", "  "}
		}
	})
	result, err := s.BulkMerge(t.Context(), "admin", matching.BulkMergeRequest{Mode: "title_artist"}, "bulk-label-normalization")
	if err != nil || result.MergedGroups != 1 || result.MergedSources != 3 || result.Revision != 1 {
		t.Fatalf("incorrect metadata grouping: %+v %v", result, err)
	}
	for i, track := range tracks {
		link, err := s.GetLink(t.Context(), "admin", track.ID)
		if err != nil || (link.RecordingID != nil) != (i < 2 || i == 3) {
			t.Fatalf("incorrect metadata membership %d: %+v %v", i, link, err)
		}
	}
}

func TestMatchingBulkMergeEmptyReceiptDoesNotAdvanceRevision(t *testing.T) {
	s, _, tracks := bulkMergeFixture(t, 2, nil)
	request := bulkScoreRequest(0, 100)
	result, err := s.BulkMerge(t.Context(), "admin", request, "bulk-empty-receipt")
	if err != nil || result.Revision != 0 || result.OperationID == "" || result.MergedGroups != 0 || result.MergedSources != 0 || len(result.Changes) != 0 {
		t.Fatalf("invalid empty result: %+v %v", result, err)
	}
	if _, err = s.ManualLink(t.Context(), "admin", matching.LinkRequest{SourceTrackIDs: []string{tracks[0].ID, tracks[1].ID}}, "bulk-after-empty"); err != nil {
		t.Fatal(err)
	}
	replay, err := s.BulkMerge(t.Context(), "admin", request, "bulk-empty-receipt")
	if err != nil || !reflect.DeepEqual(result, replay) {
		t.Fatal("empty receipt was recomputed after later changes", err)
	}
	page, err := s.ChangesAfter(t.Context(), "admin", 0, 50)
	if err != nil || page.Revision != 1 || len(page.Items) != 2 {
		t.Fatal("empty action created audit entries", err)
	}
}
