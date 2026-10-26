package storage_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/library/personal"
	"github.com/fnMrRice/MusicFS-server/src/library/playlists"
	"github.com/fnMrRice/MusicFS-server/src/metadata/matching"
)

func actionSelection(t *testing.T, store *sqlite.Store) library.TrackSelection {
	t.Helper()
	var revision string
	if err := store.DB.QueryRow("SELECT revision FROM library_revision").Scan(&revision); err != nil {
		t.Fatal(err)
	}
	return library.TrackSelection{Batch: true, LibraryRevision: revision}
}

func TestFavoritesSelectionCoversUnloadedRowsAndPreservesExistingTime(t *testing.T) {
	s, _, tracks := matchingFixture(t, 125)
	ctx := context.Background()
	private := matchingPrivateTrack(t, s)
	first, err := s.PutFavorite(ctx, "admin", tracks[1].ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PutFavorite(ctx, "bob", private.ID); err != nil {
		t.Fatal(err)
	}
	selection := actionSelection(t, s)
	selection.Exclude = []string{tracks[0].ID}
	result, err := s.SetFavoritesSelection(ctx, "admin", selection, true)
	if err != nil || result.Affected != 123 || len(result.SourceTrackIDs) != 124 {
		t.Fatalf("batch favorite: %+v %v", result, err)
	}
	again, err := s.SetFavoritesSelection(ctx, "admin", selection, true)
	if err != nil || again.Affected != 0 || !reflect.DeepEqual(again.SourceTrackIDs, result.SourceTrackIDs) {
		t.Fatalf("repeat favorite: %+v %v", again, err)
	}
	unchanged, err := s.PutFavorite(ctx, "admin", tracks[1].ID)
	if err != nil || !first.RecordedAt.Equal(unchanged.RecordedAt) {
		t.Fatal("favorite timestamp changed", err)
	}
	deleted, err := s.SetFavoritesSelection(ctx, "admin", selection, false)
	if err != nil || deleted.Affected != 124 || !reflect.DeepEqual(deleted.SourceTrackIDs, result.SourceTrackIDs) {
		t.Fatalf("batch remove: %+v %v", deleted, err)
	}
	privatePage, err := s.PersonalCollection(ctx, "bob", personal.Favorites, "", 10)
	if err != nil || len(privatePage.Items) != 1 {
		t.Fatal("private favorites changed", err)
	}
	selection.Exclude = []string{private.ID}
	_, err = s.SetFavoritesSelection(ctx, "admin", selection, true)
	matchingError(t, err, "NOT_FOUND")
	page, err := s.PersonalCollection(ctx, "admin", personal.Favorites, "", 10)
	if err != nil || len(page.Items) != 0 {
		t.Fatal("invalid selection wrote favorites", err)
	}
}

func TestFavoritesSelectionChecksLimitBeforeAnyWrite(t *testing.T) {
	s, _, tracks := matchingFixture(t, 3)
	ctx := context.Background()
	_, err := s.DB.Exec("WITH RECURSIVE n(v) AS (SELECT 1 UNION ALL SELECT v+1 FROM n WHERE v<19999) INSERT INTO personal_favorites(owner,source_track_id,created_at) SELECT 'admin','seed-'||v,'2026-01-01T00:00:00.000000000Z' FROM n")
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.SetFavoritesSelection(ctx, "admin", actionSelection(t, s), true)
	matchingError(t, err, "RATE_LIMITED")
	var count int
	if err = s.DB.QueryRow("SELECT COUNT(*) FROM personal_favorites WHERE owner='admin'").Scan(&count); err != nil || count != 19999 {
		t.Fatal("limit failure partially wrote favorites", count, err)
	}
	for _, track := range tracks {
		if err = s.DB.QueryRow("SELECT COUNT(*) FROM personal_favorites WHERE owner='admin' AND source_track_id=?", track.ID).Scan(&count); err != nil || count != 0 {
			t.Fatal("new favorite escaped rollback", err)
		}
	}
}

func TestPlaylistSelectionCreatesAppendsAndReplaysAtomically(t *testing.T) {
	s, _, tracks := matchingFixture(t, 125)
	ctx := context.Background()
	selection := actionSelection(t, s)
	selection.Exclude = []string{tracks[0].ID}
	request := playlists.CreateSelectionRequest{Name: "全选歌单", Selection: selection}
	created, err := s.CreatePlaylistSelection(ctx, "admin", request, "selection-create")
	if err != nil || created.Count != 124 || created.RuleRevision != 1 {
		t.Fatalf("create selection: %+v %v", created, err)
	}
	replayed, err := s.CreatePlaylistSelection(ctx, "admin", request, "selection-create")
	if err != nil || replayed != created {
		t.Fatal("create replay changed receipt", err)
	}
	appendRequest := playlists.AppendSelectionRequest{ExpectedRevision: 1, Selection: library.TrackSelection{SourceTrackIDs: []string{tracks[0].ID}}}
	appended, err := s.AppendPlaylistSelection(ctx, "admin", created.PlaylistID, appendRequest, "selection-append")
	if err != nil || appended.Count != 1 || appended.RuleRevision != 2 {
		t.Fatalf("append selection: %+v %v", appended, err)
	}
	replayed, err = s.AppendPlaylistSelection(ctx, "admin", created.PlaylistID, appendRequest, "selection-append")
	if err != nil || replayed != appended {
		t.Fatal("append replay changed receipt", err)
	}
	_, err = s.AppendPlaylistSelection(ctx, "admin", created.PlaylistID, appendRequest, "selection-stale")
	matchingError(t, err, "CONFLICT")
	_, err = s.AppendPlaylistSelection(ctx, "bob", created.PlaylistID, appendRequest, "selection-private")
	matchingError(t, err, "NOT_FOUND")
	playlist, err := s.Playlist(ctx, "admin", created.PlaylistID)
	if err != nil || len(playlist.Bindings) != 2 || len(playlist.Bindings[0].Entries) != 124 || len(playlist.Bindings[1].Entries) != 1 || playlist.RuleRevision != 2 {
		t.Fatal("append failed to retain original bindings", err)
	}
	if _, err = s.DB.Exec("UPDATE library_revision SET revision=revision+1"); err != nil {
		t.Fatal(err)
	}
	_, err = s.CreatePlaylistSelection(ctx, "admin", request, "selection-stale-library")
	matchingError(t, err, "CONFLICT")
	all, err := s.Playlists(ctx, "admin", "", 100)
	if err != nil || len(all) != 1 {
		t.Fatal("stale create partially wrote playlist", err)
	}
}

func TestMatchingSelectionExpandsGroupedVersionsAndKeepsLimit(t *testing.T) {
	s, _, tracks := matchingFixture(t, 3)
	ctx := context.Background()
	linked, err := s.ManualLink(ctx, "admin", matching.LinkRequest{SourceTrackIDs: []string{tracks[0].ID, tracks[1].ID}}, domain.NewID())
	if err != nil {
		t.Fatal(err)
	}
	selection := actionSelection(t, s)
	selection.GroupBy, selection.MatchingRevision = "recording", &linked.Revision
	request := matching.LinkRequest{ExpectedRevision: linked.Revision, Selection: &selection}
	result, err := s.ManualLink(ctx, "admin", request, "selection-link")
	if err != nil || len(result.Changes) != 3 {
		t.Fatalf("grouped link: %+v %v", result, err)
	}
	replayed, err := s.ManualLink(ctx, "admin", request, "selection-link")
	if err != nil || replayed.OperationID != result.OperationID {
		t.Fatal("matching replay failed", err)
	}
	request.SourceTrackIDs = []string{tracks[0].ID}
	_, err = s.ManualLink(ctx, "admin", request, "selection-ambiguous")
	matchingError(t, err, "INVALID_ARGUMENT")
	large, _, _ := matchingFixture(t, 257)
	tooMany := actionSelection(t, large)
	_, err = large.ManualLink(ctx, "admin", matching.LinkRequest{Selection: &tooMany}, "selection-too-large")
	matchingError(t, err, "RATE_LIMITED")
	revision, err := large.MatchingRevision(ctx, "admin")
	if err != nil || revision != 0 {
		t.Fatal("oversize selection partially linked", err)
	}
}
