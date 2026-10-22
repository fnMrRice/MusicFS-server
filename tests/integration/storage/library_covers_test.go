package storage_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/library/catalog"
	"github.com/fnMrRice/MusicFS-server/src/metadata/matching"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore"
)

func libraryArtwork(t *testing.T, s *sqlite.Store, owner string, track library.Track) mediastore.ContentObject {
	t.Helper()
	input := contentInput(track, "", "verified image bytes")
	input.Metadata.Field, input.Metadata.Format, input.MIME = "artwork", "png", "image/png"
	return contentPublish(t, s, owner, input)
}

func expectTrackCover(t *testing.T, track library.Track, object string) {
	t.Helper()
	if object == "" {
		if track.CoverObjectID != nil {
			t.Fatalf("unexpected cover for %s: %s", track.ID, *track.CoverObjectID)
		}
	} else if track.CoverObjectID == nil || *track.CoverObjectID != object {
		t.Fatalf("cover mismatch for %s: got %v, want %s", track.ID, track.CoverObjectID, object)
	}
}

func expectLibraryCovers(t *testing.T, s *sqlite.Store, owner string, expected map[string]string) {
	t.Helper()
	ctx := context.Background()
	for id, object := range expected {
		track, err := s.FindTrack(ctx, owner, id)
		if err != nil {
			t.Fatal(err)
		}
		expectTrackCover(t, track, object)
	}
	for _, list := range []func(context.Context, string, string, string, int) (library.Page, error){s.ListTracks, s.ListGroupedTracks} {
		page, err := list(ctx, owner, "", "", 200)
		if err != nil {
			t.Fatal(err)
		}
		seen := map[string]bool{}
		for _, track := range page.Items {
			object, exists := expected[track.ID]
			if !exists {
				t.Fatalf("unexpected source or owner leak: %s", track.ID)
			}
			expectTrackCover(t, track, object)
			seen[track.ID] = true
			for _, version := range track.Versions {
				object, exists := expected[version.ID]
				if !exists {
					t.Fatalf("unexpected grouped source or owner leak: %s", version.ID)
				}
				expectTrackCover(t, version, object)
				seen[version.ID] = true
			}
		}
		if len(seen) != len(expected) {
			t.Fatalf("missing sources: got %d, want %d", len(seen), len(expected))
		}
	}
}

func TestLibraryCoversUseOnlySelectedCurrentArtworkAndPreserveOwner(t *testing.T) {
	s, _, tracks := matchingFixture(t, 3)
	ctx := context.Background()
	first := libraryArtwork(t, s, "admin", tracks[0])
	second := libraryArtwork(t, s, "admin", tracks[1])
	libraryArtwork(t, s, "admin", tracks[2])
	contentSelect(t, s, first, 0, false)
	contentSelect(t, s, second, 0, true)
	lyrics := contentPublish(t, s, "admin", contentInput(tracks[2], "en", "lyrics only"))
	contentSelect(t, s, lyrics, 0, false)
	private := matchingPrivateTrack(t, s)
	privateArt := libraryArtwork(t, s, "bob", private)
	if _, err := s.SelectContent(ctx, "bob", mediastore.SelectRequest{Target: privateArt.Metadata.Target, ObjectID: privateArt.ID}, domain.NewID()); err != nil {
		t.Fatal(err)
	}
	expectLibraryCovers(t, s, "admin", map[string]string{tracks[0].ID: first.ID, tracks[1].ID: second.ID, tracks[2].ID: ""})
	expectLibraryCovers(t, s, "bob", map[string]string{private.ID: privateArt.ID})
	if _, err := s.ManualLink(ctx, "admin", matching.LinkRequest{SourceTrackIDs: []string{tracks[0].ID, tracks[1].ID}}, domain.NewID()); err != nil {
		t.Fatal(err)
	}
	grouped, err := s.ListGroupedTracks(ctx, "admin", "", "", 200)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range grouped.Items {
		if item.RecordingID != nil {
			expectTrackCover(t, item, min(first.ID, second.ID))
			for _, version := range item.Versions {
				expectTrackCover(t, version, map[string]string{tracks[0].ID: first.ID, tracks[1].ID: second.ID}[version.ID])
			}
		}
	}
	_, err = s.FindTrack(ctx, "admin", private.ID)
	matchingError(t, err, "NOT_FOUND")
}

func TestLibraryCoversDisappearWhenContentOrSourceBecomesUnavailable(t *testing.T) {
	s, observations, tracks := matchingFixture(t, 3)
	ctx := context.Background()
	for _, track := range tracks {
		contentSelect(t, s, libraryArtwork(t, s, "admin", track), 0, false)
	}
	selection, err := s.ContentSelection(ctx, "admin", tracks[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.InvalidateContent(ctx, "admin", selection.Bindings[0].ObjectID); err != nil {
		t.Fatal(err)
	}
	observations[1].ObservedVersion = "v2"
	// 第二首更换版本，第三首从授权扫描范围中消失。
	publishEvidenceTracks(t, s, observations[:2]...)
	expectLibraryCovers(t, s, "admin", map[string]string{tracks[0].ID: "", tracks[1].ID: "", tracks[2].ID: ""})
}

func TestLibraryCoverPaginationRefreshesSelectionWithoutMixingAssetVersions(t *testing.T) {
	for _, grouped := range []bool{false, true} {
		name := "raw"
		if grouped {
			name = "grouped"
		}
		t.Run(name, func(t *testing.T) {
			s, observations, tracks := matchingFixture(t, 2)
			ctx := context.Background()
			list := s.ListTracks
			if grouped {
				list = s.ListGroupedTracks
			}
			first, err := list(ctx, "admin", "", "", 1)
			if err != nil || first.NextCursor == nil {
				t.Fatalf("missing cursor: %+v %v", first, err)
			}
			cover := libraryArtwork(t, s, "admin", tracks[1])
			contentSelect(t, s, cover, 0, false)
			tail, err := list(ctx, "admin", "", *first.NextCursor, 1)
			if err != nil || len(tail.Items) != 1 {
				t.Fatalf("missing tail: %+v %v", tail, err)
			}
			expectTrackCover(t, tail.Items[0], cover.ID)
			observations[1].ObservedVersion = "v2"
			current := publishEvidenceTracks(t, s, observations...)[1]
			selection, err := s.ContentSelection(ctx, "admin", current.ID)
			if err != nil {
				t.Fatal(err)
			}
			newCover := libraryArtwork(t, s, "admin", current)
			contentSelect(t, s, newCover, selection.Revision, false)
			tail, err = list(ctx, "admin", "", *first.NextCursor, 1)
			if err != nil || len(tail.Items) != 1 {
				t.Fatalf("missing old tail: %+v %v", tail, err)
			}
			expectTrackCover(t, tail.Items[0], "")
			for _, version := range tail.Items[0].Versions {
				expectTrackCover(t, version, "")
			}
			expectLibraryCovers(t, s, "admin", map[string]string{tracks[0].ID: "", tracks[1].ID: newCover.ID})
		})
	}
}

func TestCatalogTrackCoversRecheckLiveSelectionOnSnapshotPages(t *testing.T) {
	s, _, tracks := matchingFixture(t, 2)
	ctx := context.Background()
	cover := libraryArtwork(t, s, "admin", tracks[1])
	contentSelect(t, s, cover, 0, false)
	first, err := s.CreateCatalogQuery(ctx, "admin", catalog.Query{Type: "tracks"}, 1, domain.NewID())
	if err != nil || first.NextCursor == nil {
		t.Fatalf("missing catalog cursor: %+v %v", first, err)
	}
	tail, err := s.CatalogQueryPage(ctx, "admin", first.QueryID, *first.NextCursor, 1, nil)
	if err != nil || len(tail.Items) != 1 {
		t.Fatalf("missing catalog tail: %+v %v", tail, err)
	}
	var track catalog.Track
	if err = json.Unmarshal(tail.Items[0], &track); err != nil {
		t.Fatal(err)
	}
	expectTrackCover(t, track.Track, cover.ID)
	if err = s.InvalidateContent(ctx, "admin", cover.ID); err != nil {
		t.Fatal(err)
	}
	tail, err = s.CatalogQueryPage(ctx, "admin", first.QueryID, *first.NextCursor, 1, nil)
	if err != nil || len(tail.Items) != 1 {
		t.Fatalf("missing catalog retry: %+v %v", tail, err)
	}
	if err = json.Unmarshal(tail.Items[0], &track); err != nil {
		t.Fatal(err)
	}
	expectTrackCover(t, track.Track, "")
}

func TestLibraryCoverSurvivesVerifiedWritebackRebinding(t *testing.T) {
	f := newWritebackFixture(t)
	ctx := context.Background()
	cover := libraryArtwork(t, f.s, "admin", f.track)
	selection := contentSelect(t, f.s, cover, 2, false)
	f.request.ExpectedRevision = selection.Revision
	job := f.queue(t)
	if err := f.s.BeginWriteback(ctx, "admin", job.ID, f.target, selection.Revision); err != nil {
		t.Fatal(err)
	}
	result, observation := f.result(job, true)
	if _, err := f.s.CommitWriteback(ctx, "admin", job.ID, result, observation); err != nil {
		t.Fatal(err)
	}
	expectLibraryCovers(t, f.s, "admin", map[string]string{f.track.ID: cover.ID})
}
