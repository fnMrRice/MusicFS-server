package storage_test

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/metadata/matching"
)

func TestDefaultSourceQualityManualCancellationAndPersistence(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "defaults.sqlite")
	s, err := sqlite.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	items := []library.Observation{}
	for i, rate := range []int64{128000, 320000, 320000, 0, 0} {
		item := evidenceObservation(string(rune('a'+i)), "v1")
		if rate > 0 {
			item.BitrateBps = &rate
		}
		item.SizeBytes = []int64{9000, 2000, 3000, 999999, 1000}[i]
		items = append(items, item)
	}
	tracks := publishEvidenceTracks(t, s, items...)
	ids := []string{}
	for _, track := range tracks {
		ids = append(ids, track.ID)
	}
	linked, err := s.ManualLink(ctx, "admin", matching.LinkRequest{SourceTrackIDs: ids}, "quality-link-sources")
	if err != nil {
		t.Fatal(err)
	}
	group := func() library.Track {
		t.Helper()
		page, err := s.ListGroupedTracks(ctx, "admin", "", "", 100)
		if err != nil || len(page.Items) != 1 {
			t.Fatalf("grouped read: %+v %v", page, err)
		}
		return page.Items[0]
	}
	initial := group()
	names := []string{}
	for _, version := range initial.Versions {
		names = append(names, version.DisplayName)
	}
	if initial.ID != ids[2] || initial.ManualSourceTrackID != nil || !reflect.DeepEqual(names, []string{"c", "b", "a", "d", "e"}) {
		t.Fatalf("quality order or automatic default incorrect: %+v %v", initial, names)
	}
	request := matching.DefaultSourceRequest{ExpectedRevision: 1, RecordingID: *linked.RecordingID, SourceTrackID: &ids[0]}
	chosen, err := s.SetDefaultSource(ctx, "admin", request, "quality-manual-source")
	if err != nil || chosen.Revision != 2 || len(chosen.Changes) != 1 || chosen.Changes[0].Operation != "defaultSource" {
		t.Fatalf("manual selection: %+v %v", chosen, err)
	}
	replay, err := s.SetDefaultSource(ctx, "admin", request, "quality-manual-source")
	if err != nil || !reflect.DeepEqual(chosen, replay) {
		t.Fatalf("idempotency changed selection: %+v %v", replay, err)
	}
	_, err = s.SetDefaultSource(ctx, "admin", request, "quality-stale-choice")
	matchingError(t, err, "CONFLICT")
	if got := group(); got.ID != ids[0] || got.ManualSourceTrackID == nil || *got.ManualSourceTrackID != ids[0] {
		t.Fatalf("manual default ignored: %+v", got)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = sqlite.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if got := group(); got.ID != ids[0] || got.ManualSourceTrackID == nil {
		t.Fatal("manual preference was not persisted")
	}
	// 扫描发现来源暂时消失时，不自动覆盖手动指定。
	publishEvidenceTracks(t, s, items[1:]...)
	if got := group(); got.ID != ids[0] || got.Availability != "missing" {
		t.Fatalf("missing manual source was replaced: %+v", got)
	}
	request.ExpectedRevision, request.SourceTrackID = 2, nil
	cleared, err := s.SetDefaultSource(ctx, "admin", request, "quality-clear-default")
	if err != nil || cleared.Revision != 3 || cleared.Changes[0].SourceTrackID != nil {
		t.Fatalf("clear preference: %+v %v", cleared, err)
	}
	if got := group(); got.ID != ids[2] || got.ManualSourceTrackID != nil {
		t.Fatalf("automatic default not restored: %+v", got)
	}
	// 可用性优先于较高码率；失效成员仍完整保留在版本列表中。
	publishEvidenceTracks(t, s, items[3:]...)
	if got := group(); got.ID != ids[3] || len(got.Versions) != len(items) {
		t.Fatalf("available unknown-rate fallback missing: %+v", got)
	}
}

func TestDefaultSourceOwnershipMembershipAndMovedMemberCleanup(t *testing.T) {
	s, _, tracks := matchingFixture(t, 3)
	ctx := context.Background()
	private := matchingPrivateTrack(t, s)
	linked, err := s.ManualLink(ctx, "admin", matching.LinkRequest{SourceTrackIDs: []string{tracks[0].ID, tracks[1].ID}}, "default-member-link")
	if err != nil {
		t.Fatal(err)
	}
	request := matching.DefaultSourceRequest{ExpectedRevision: 1, RecordingID: *linked.RecordingID, SourceTrackID: &tracks[2].ID}
	_, err = s.SetDefaultSource(ctx, "admin", request, "default-non-member")
	matchingError(t, err, "INVALID_ARGUMENT")
	request.SourceTrackID = &private.ID
	_, err = s.SetDefaultSource(ctx, "admin", request, "default-private-source")
	matchingError(t, err, "NOT_FOUND")
	request.SourceTrackID = nil
	request.ExpectedRevision = 0
	_, err = s.SetDefaultSource(ctx, "bob", request, "default-private-recording")
	matchingError(t, err, "NOT_FOUND")
	request.ExpectedRevision, request.SourceTrackID = 1, &tracks[0].ID
	if _, err = s.SetDefaultSource(ctx, "admin", request, "default-valid-member"); err != nil {
		t.Fatal(err)
	}
	split, err := s.Split(ctx, "admin", matching.SplitRequest{ExpectedRevision: 2, RecordingID: request.RecordingID, SourceTrackIDs: []string{tracks[0].ID}}, "default-split-member")
	if err != nil {
		t.Fatal(err)
	}
	for _, recordingID := range []string{request.RecordingID, *split.RecordingID} {
		recording, err := s.Recording(ctx, "admin", recordingID)
		if err != nil || recording.ManualSourceTrackID != nil {
			t.Fatalf("moved default leaked across recordings: %+v %v", recording, err)
		}
	}
	request.ExpectedRevision, request.RecordingID = 3, *split.RecordingID
	if _, err = s.SetDefaultSource(ctx, "admin", request, "default-split-selected"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Unlink(ctx, "admin", matching.UnlinkRequest{ExpectedRevision: 4, SourceTrackID: tracks[0].ID}, "default-unlink-selected"); err != nil {
		t.Fatal(err)
	}
	recording, err := s.Recording(ctx, "admin", request.RecordingID)
	if err != nil || recording.ManualSourceTrackID != nil {
		t.Fatalf("unlinked default retained: %+v %v", recording, err)
	}
}

func TestDefaultSourceNewSelectionDoesNotRewriteGroupedCursor(t *testing.T) {
	s := evidenceStore(t)
	ctx := context.Background()
	items := []library.Observation{evidenceObservation("a-single", "v1"), evidenceObservation("z-lower", "v1"), evidenceObservation("z-higher", "v1")}
	low, high := int64(128000), int64(320000)
	items[1].BitrateBps, items[2].BitrateBps = &low, &high
	tracks := publishEvidenceTracks(t, s, items...)
	byName := map[string]string{}
	for _, track := range tracks {
		byName[track.DisplayName] = track.ID
	}
	linked, err := s.ManualLink(ctx, "admin", matching.LinkRequest{SourceTrackIDs: []string{byName["z-lower"], byName["z-higher"]}}, "default-cursor-link")
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.ListGroupedTracks(ctx, "admin", "", "", 1)
	if err != nil || first.NextCursor == nil {
		t.Fatalf("missing initial cursor: %+v %v", first, err)
	}
	preferred := byName["z-lower"]
	_, err = s.SetDefaultSource(ctx, "admin", matching.DefaultSourceRequest{ExpectedRevision: 1, RecordingID: *linked.RecordingID, SourceTrackID: &preferred}, "default-cursor-change")
	if err != nil {
		t.Fatal(err)
	}
	old, err := s.ListGroupedTracks(ctx, "admin", "", *first.NextCursor, 100)
	if err != nil || len(old.Items) != 1 || old.Items[0].ID != byName["z-higher"] || old.Items[0].ManualSourceTrackID != nil || old.MatchingRevision == nil || *old.MatchingRevision != 1 {
		t.Fatalf("existing cursor mixed default revisions: %+v %v", old, err)
	}
	current, err := s.ListGroupedTracks(ctx, "admin", "", "", 100)
	if err != nil || len(current.Items) != 2 || current.Items[1].ID != preferred || current.Items[1].ManualSourceTrackID == nil || current.MatchingRevision == nil || *current.MatchingRevision != 2 {
		t.Fatalf("fresh query lost selected default: %+v %v", current, err)
	}
}

func TestDefaultSourceMovingToExistingGroupPreservesItsOwnPreference(t *testing.T) {
	s, _, tracks := matchingFixture(t, 4)
	ctx := context.Background()
	first, err := s.ManualLink(ctx, "admin", matching.LinkRequest{SourceTrackIDs: []string{tracks[0].ID, tracks[1].ID}}, "default-merge-first")
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.ManualLink(ctx, "admin", matching.LinkRequest{ExpectedRevision: 1, SourceTrackIDs: []string{tracks[2].ID, tracks[3].ID}}, "default-merge-second")
	if err != nil {
		t.Fatal(err)
	}
	for i, request := range []matching.DefaultSourceRequest{
		{ExpectedRevision: 2, RecordingID: *first.RecordingID, SourceTrackID: &tracks[0].ID},
		{ExpectedRevision: 3, RecordingID: *second.RecordingID, SourceTrackID: &tracks[3].ID},
	} {
		key := []string{"default-merge-prefer-first", "default-merge-prefer-second"}[i]
		if _, err = s.SetDefaultSource(ctx, "admin", request, key); err != nil {
			t.Fatal(err)
		}
	}
	_, err = s.ManualLink(ctx, "admin", matching.LinkRequest{ExpectedRevision: 4, SourceTrackIDs: []string{tracks[0].ID}, TargetRecordingID: second.RecordingID}, "default-merge-move")
	if err != nil {
		t.Fatal(err)
	}
	old, err := s.Recording(ctx, "admin", *first.RecordingID)
	if err != nil || old.ManualSourceTrackID != nil {
		t.Fatalf("old group retained moved default: %+v %v", old, err)
	}
	target, err := s.Recording(ctx, "admin", *second.RecordingID)
	if err != nil || target.ManualSourceTrackID == nil || *target.ManualSourceTrackID != tracks[3].ID {
		t.Fatalf("target group preference changed: %+v %v", target, err)
	}
}
