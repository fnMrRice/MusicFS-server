package storage_test

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/library/catalog"
	"github.com/fnMrRice/MusicFS-server/src/metadata/matching"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore"
)

func recordingArtwork(t *testing.T, s *sqlite.Store, track library.Track, width, height int) mediastore.ContentObject {
	t.Helper()
	input := contentInput(track, "", fmt.Sprintf("verified artwork %dx%d", width, height))
	input.Metadata.Field, input.Metadata.Format, input.MIME = "artwork", "png", "image/png"
	input.Width, input.Height = width, height
	object := contentPublish(t, s, "admin", input)
	selection, err := s.ContentSelection(context.Background(), "admin", track.ID)
	if err != nil {
		t.Fatal(err)
	}
	contentSelect(t, s, object, selection.Revision, false)
	return object
}

func recordingCoverGroup(t *testing.T, s *sqlite.Store, ids ...string) string {
	t.Helper()
	ctx := context.Background()
	revision, err := s.MatchingRevision(ctx, "admin")
	if err != nil {
		t.Fatal(err)
	}
	linked, err := s.ManualLink(ctx, "admin", matching.LinkRequest{ExpectedRevision: revision, SourceTrackIDs: ids}, domain.NewID())
	if err != nil || linked.RecordingID == nil {
		t.Fatalf("recording link failed: %+v %v", linked, err)
	}
	return *linked.RecordingID
}

func expectRecordingCover(t *testing.T, result library.RecordingCover, automatic, manual string) {
	t.Helper()
	if automatic == "" && result.CoverObjectID != nil || automatic != "" && (result.CoverObjectID == nil || *result.CoverObjectID != automatic) {
		t.Fatalf("unexpected selected recording cover: %+v", result)
	}
	if manual == "" && result.ManualObjectID != nil || manual != "" && (result.ManualObjectID == nil || *result.ManualObjectID != manual) {
		t.Fatalf("unexpected manual recording cover: %+v", result)
	}
}

func TestRecordingCoversChooseActualResolutionAndProjectTopLevelOnly(t *testing.T) {
	s, _, tracks := matchingFixture(t, 4)
	ctx := context.Background()
	covers := []mediastore.ContentObject{
		recordingArtwork(t, s, tracks[0], 0, 0),
		recordingArtwork(t, s, tracks[1], 800, 200),
		recordingArtwork(t, s, tracks[2], 400, 400),
		recordingArtwork(t, s, tracks[3], 400, 400),
	}
	id := recordingCoverGroup(t, s, tracks[0].ID, tracks[1].ID, tracks[2].ID, tracks[3].ID)
	result, err := s.RecordingCover(ctx, "admin", id)
	if err != nil || len(result.Candidates) != 4 || result.Revision != 0 {
		t.Fatalf("recording covers unavailable: %+v %v", result, err)
	}
	best := min(covers[2].ID, covers[3].ID)
	expectRecordingCover(t, result, best, "")
	if result.Candidates[2].ObjectID != covers[1].ID || result.Candidates[3].ObjectID != covers[0].ID {
		t.Fatal("resolution order did not use area, short side, and known dimensions", result)
	}
	for _, source := range tracks {
		track, err := s.FindTrack(ctx, "admin", source.ID)
		if err != nil || track.RecordingID == nil || *track.RecordingID != id {
			t.Fatalf("recording association missing: %+v %v", track, err)
		}
		expectTrackCover(t, track, best)
	}
	page, err := s.ListGroupedTracks(ctx, "admin", "", "", 200)
	if err != nil || len(page.Items) != 1 || len(page.Items[0].Versions) != 4 {
		t.Fatalf("grouped recording missing: %+v %v", page, err)
	}
	expectTrackCover(t, page.Items[0], best)
	byID := map[string]string{}
	for i, track := range tracks {
		byID[track.ID] = covers[i].ID
	}
	for _, version := range page.Items[0].Versions {
		expectTrackCover(t, version, byID[version.ID])
	}
	catalogPage, err := s.CreateCatalogQuery(ctx, "admin", catalog.Query{Type: "tracks"}, 200, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range catalogPage.Items {
		var track catalog.Track
		if err = json.Unmarshal(raw, &track); err != nil {
			t.Fatal(err)
		}
		expectTrackCover(t, track.Track, best)
	}
}

func TestRecordingCoversManualRevisionReplayAuthorizationAndReset(t *testing.T) {
	s, _, tracks := matchingFixture(t, 3)
	ctx := context.Background()
	small := recordingArtwork(t, s, tracks[0], 64, 64)
	large := recordingArtwork(t, s, tracks[1], 512, 512)
	unrelated := recordingArtwork(t, s, tracks[2], 1024, 1024)
	id := recordingCoverGroup(t, s, tracks[0].ID, tracks[1].ID)
	request := library.SelectRecordingCoverRequest{RecordingID: id, ObjectID: &small.ID}
	tooLarge := request
	tooLarge.ExpectedRevision = math.MaxInt64
	_, err := s.SelectRecordingCover(ctx, "admin", tooLarge, domain.NewID())
	matchingError(t, err, "INVALID_ARGUMENT")
	first, err := s.SelectRecordingCover(ctx, "admin", request, "recording-cover-manual")
	if err != nil || first.Revision != 1 {
		t.Fatalf("manual selection failed: %+v %v", first, err)
	}
	expectRecordingCover(t, first, small.ID, small.ID)
	replay, err := s.SelectRecordingCover(ctx, "admin", request, "recording-cover-manual")
	if err != nil || !reflect.DeepEqual(first, replay) {
		t.Fatal("manual retry did not replay", replay, err)
	}
	request.ObjectID = &large.ID
	_, err = s.SelectRecordingCover(ctx, "admin", request, "recording-cover-manual")
	matchingError(t, err, "CONFLICT")
	_, err = s.SelectRecordingCover(ctx, "admin", request, domain.NewID())
	matchingError(t, err, "CONFLICT")
	request.ExpectedRevision = 1
	request.ObjectID = &unrelated.ID
	_, err = s.SelectRecordingCover(ctx, "admin", request, domain.NewID())
	matchingError(t, err, "RESOURCE_CHANGED")
	_, err = s.RecordingCover(ctx, "bob", id)
	matchingError(t, err, "NOT_FOUND")
	_, err = s.SelectRecordingCover(ctx, "bob", request, domain.NewID())
	matchingError(t, err, "NOT_FOUND")
	request.ObjectID = nil
	reset, err := s.SelectRecordingCover(ctx, "admin", request, domain.NewID())
	if err != nil || reset.Revision != 2 {
		t.Fatalf("reset failed: %+v %v", reset, err)
	}
	expectRecordingCover(t, reset, large.ID, "")
	revision, err := s.MatchingRevision(ctx, "admin")
	if err != nil || revision != 1 {
		t.Fatal("cover preference changed matching revision", revision, err)
	}
}

func TestRecordingCoversPreserveInvalidManualChoiceAndRejectMovedMembers(t *testing.T) {
	s, _, tracks := matchingFixture(t, 3)
	ctx := context.Background()
	small := recordingArtwork(t, s, tracks[0], 64, 64)
	large := recordingArtwork(t, s, tracks[1], 512, 512)
	id := recordingCoverGroup(t, s, tracks[0].ID, tracks[1].ID)
	request := library.SelectRecordingCoverRequest{RecordingID: id, ObjectID: &large.ID}
	if _, err := s.SelectRecordingCover(ctx, "admin", request, domain.NewID()); err != nil {
		t.Fatal(err)
	}
	if err := s.InvalidateContent(ctx, "admin", large.ID); err != nil {
		t.Fatal(err)
	}
	result, err := s.RecordingCover(ctx, "admin", id)
	if err != nil || result.Revision != 1 || len(result.Candidates) != 1 {
		t.Fatalf("unavailable object remained a candidate: %+v %v", result, err)
	}
	expectRecordingCover(t, result, small.ID, large.ID)
	request.ExpectedRevision, request.ObjectID = 1, &small.ID
	if _, err = s.SelectRecordingCover(ctx, "admin", request, domain.NewID()); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Split(ctx, "admin", matching.SplitRequest{ExpectedRevision: 1, RecordingID: id, SourceTrackIDs: []string{tracks[0].ID}}, domain.NewID()); err != nil {
		t.Fatal(err)
	}
	result, err = s.RecordingCover(ctx, "admin", id)
	if err != nil {
		t.Fatal(err)
	}
	expectRecordingCover(t, result, "", small.ID)
	request.ExpectedRevision = 2
	_, err = s.SelectRecordingCover(ctx, "admin", request, domain.NewID())
	matchingError(t, err, "RESOURCE_CHANGED")
}

func TestRecordingCoverSnapshotExcludesNewMembersAndChangedAssets(t *testing.T) {
	s, observations, tracks := matchingFixture(t, 3)
	ctx := context.Background()
	newMember := recordingArtwork(t, s, tracks[0], 1024, 1024)
	small := recordingArtwork(t, s, tracks[1], 64, 64)
	large := recordingArtwork(t, s, tracks[2], 512, 512)
	id := recordingCoverGroup(t, s, tracks[1].ID, tracks[2].ID)
	first, err := s.ListGroupedTracks(ctx, "admin", "", "", 1)
	if err != nil || first.NextCursor == nil {
		t.Fatalf("missing grouped cursor: %+v %v", first, err)
	}
	if _, err = s.ManualLink(ctx, "admin", matching.LinkRequest{ExpectedRevision: 1, SourceTrackIDs: []string{tracks[0].ID}, TargetRecordingID: &id}, domain.NewID()); err != nil {
		t.Fatal(err)
	}
	current, err := s.RecordingCover(ctx, "admin", id)
	if err != nil {
		t.Fatal(err)
	}
	expectRecordingCover(t, current, newMember.ID, "")
	tail, err := s.ListGroupedTracks(ctx, "admin", "", *first.NextCursor, 200)
	if err != nil || len(tail.Items) != 1 {
		t.Fatalf("missing grouped tail: %+v %v", tail, err)
	}
	expectTrackCover(t, tail.Items[0], large.ID)
	observations[2].ObservedVersion = "v2"
	refreshed := publishEvidenceTracks(t, s, observations...)
	recordingArtwork(t, s, refreshed[2], 2048, 2048)
	tail, err = s.ListGroupedTracks(ctx, "admin", "", *first.NextCursor, 200)
	if err != nil {
		t.Fatal(err)
	}
	expectTrackCover(t, tail.Items[0], small.ID)
}
