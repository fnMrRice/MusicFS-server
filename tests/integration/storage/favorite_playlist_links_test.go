package storage_test

import (
	"path/filepath"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite/migrations"
	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/src/library/personal"
)

func TestFavoritePlaylistLinkQueuesOnlyNewFavorites(t *testing.T) {
	s, _, _ := matchingFixture(t, 1)
	ctx := t.Context()
	b := remoteBinding()
	b.AccountID = "account"
	var ids []string
	for _, external := range []string{"80", "81", "82"} {
		track, err := s.PublishOnlineTrack(ctx, "admin", b.InstanceID, online.Track{ExternalID: external, Title: external})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, track.ID)
	}
	if _, err := s.PutFavorite(ctx, "admin", ids[0]); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		link, err := s.LinkPlaylist(ctx, "admin", b, personal.Favorites)
		if err != nil || link.PlaylistID != personal.Favorites || link.PendingCount != 0 {
			t.Fatalf("idempotent favorites link: %+v %v", link, err)
		}
	}
	_, err := s.LinkPlaylist(ctx, "other", b, personal.Favorites)
	matchingError(t, err, "CONFLICT")
	_, err = s.LinkPlaylist(ctx, "admin", b, personal.Recent)
	matchingError(t, err, "NOT_FOUND")
	if _, err := s.SetFavoriteSource(ctx, "admin", ids[1], &ids[1]); err != nil {
		t.Fatal(err)
	}
	first, err := s.NextPlaylistAddition(ctx)
	if err != nil || first == nil || first.TrackID != "81" || first.Owner != "admin" {
		t.Fatalf("unexpected favorite addition: %+v %v", first, err)
	}
	if err := s.FinishPlaylistAddition(ctx, first.ID, "", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetFavoriteSource(ctx, "admin", ids[1], nil); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := s.SetFavoritesSelection(ctx, "admin", actionSelection(t, s), true); err != nil {
			t.Fatal(err)
		}
	}
	second, err := s.NextPlaylistAddition(ctx)
	if err != nil || second == nil || second.TrackID != "82" {
		t.Fatalf("unexpected batch addition: %+v %v", second, err)
	}
	if err := s.FinishPlaylistAddition(ctx, second.ID, "RATE_LIMITED", 0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetFavoritesSelection(ctx, "admin", actionSelection(t, s), false); err != nil {
		t.Fatal(err)
	}
	links, err := s.PlaylistLinks(ctx, "admin")
	if err != nil || len(links) != 1 || links[0].PendingCount != 1 || links[0].LastErrorCode != "RATE_LIMITED" {
		t.Fatalf("unexpected pending favorites: %+v %v", links, err)
	}
	if links, err = s.PlaylistLinks(ctx, "other"); err != nil || len(links) != 0 {
		t.Fatalf("favorites link leaked across owners: %+v %v", links, err)
	}
	for range 2 {
		if err := s.UnlinkPlaylist(ctx, "admin", b.RemoteRef, personal.Favorites); err != nil {
			t.Fatal(err)
		}
	}
	if next, err := s.NextPlaylistAddition(ctx); err != nil || next != nil {
		t.Fatalf("unlink retained a pending favorite: %+v %v", next, err)
	}
}

func TestFavoritePlaylistLinkMigrationPreservesExistingQueue(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "library.sqlite")
	db := openMigrationFixture(t, path)
	exec := func(statement string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, statement, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec("CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY)")
	steps, err := migrations.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range steps {
		if step.Version > 45 {
			break
		}
		exec(step.SQL)
		exec("INSERT INTO schema_migrations VALUES(?)", step.Version)
	}
	exec("INSERT INTO personal_playlists(id,owner,rule_revision,hide_revision,snapshot_revision,document) VALUES('local','admin',1,1,1,'{}')")
	exec("INSERT INTO playlist_links(id,owner,platform,instance_id,account_id,external_id,playlist_id) VALUES(7,'admin','netease','instance','account','51','local')")
	exec("INSERT INTO playlist_link_additions(id,link_id,track_id,attempts,retry_at,last_error) VALUES(12,7,'41',3,5,'RATE_LIMITED')")
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := sqlite.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	links, err := store.PlaylistLinks(ctx, "admin")
	if err != nil || len(links) != 1 || links[0].PlaylistID != "local" || links[0].LastErrorCode != "RATE_LIMITED" {
		t.Fatalf("migration lost existing link: %+v %v", links, err)
	}
	next, err := store.NextPlaylistAddition(ctx)
	if err != nil || next == nil || next.ID != 12 || next.Attempts != 3 || next.TrackID != "41" {
		t.Fatalf("migration lost existing pending delivery: %+v %v", next, err)
	}
	if _, err = store.DB.ExecContext(ctx, "DELETE FROM personal_playlists WHERE id='local'"); err != nil {
		t.Fatal(err)
	}
	if next, err = store.NextPlaylistAddition(ctx); err != nil || next != nil {
		t.Fatalf("deleting migrated playlist retained a delivery: %+v %v", next, err)
	}
}
