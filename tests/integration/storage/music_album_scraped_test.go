package storage_test

import (
	"path/filepath"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite/migrations"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/library/music"
	"github.com/fnMrRice/MusicFS-server/src/library/works"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestMusicAlbumScrapedMigrationPersistenceAndLifetime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "albums-v40.sqlite")
	db := openMigrationFixture(t, path)
	exec := func(statement string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(t.Context(), statement, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec("CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY)")
	steps, err := migrations.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range steps {
		if step.Version > 40 {
			break
		}
		exec(step.SQL)
		exec("INSERT INTO schema_migrations VALUES(?)", step.Version)
	}
	exec("INSERT INTO music_albums(id,owner,name) VALUES('existing-album','admin','Original album')")
	exec(`INSERT INTO music_album_scraped_fields(album_id,field,document) VALUES('existing-album','albumDetails',
 '{"field":"albumDetails","pluginId":"musicfs.scraper.itunes","item":{"id":"old-release","title":"Original album","description":"Mandopop","sourceUrl":"https://music.apple.com/hk/album/original/123"}}')`)
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := sqlite.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	upgraded, err := store.MusicAlbum(t.Context(), "admin", "existing-album")
	if err != nil || upgraded.OriginalName != "Original album" {
		t.Fatalf("original album name was not backfilled: %+v %v", upgraded, err)
	}
	if len(upgraded.SourceLinks) != 1 || upgraded.SourceLinks[0].URL != "https://music.apple.com/hk/album/original/123" || upgraded.SourceLinks[0].PluginID != "musicfs.scraper.itunes" {
		t.Fatalf("album source association was not backfilled: %+v", upgraded)
	}
	var genre string
	var description any
	if err = store.DB.QueryRowContext(t.Context(), `SELECT json_extract(document,'$.item.genre'),json_extract(document,'$.item.description')
 FROM music_album_scraped_fields WHERE album_id='existing-album'`).Scan(&genre, &description); err != nil || genre != "Mandopop" || description != nil {
		t.Fatalf("iTunes genre was not migrated: %q %v %v", genre, description, err)
	}
	field := works.ScrapedField{Field: "albumDetails", PluginID: "fixture", CandidateToken: "short-lived-token",
		Item: scraper.Item{ID: "edition", Title: "Online title", Description: "Online description"}}
	revision, err := store.SelectMusicAlbumScrapedField(t.Context(), "admin", "existing-album", 1, field, "select-existing-album")
	if err != nil || revision != 2 {
		t.Fatalf("upgraded album selection: %d %v", revision, err)
	}
	if _, err = store.MusicAlbumScrapedFields(t.Context(), "other", "existing-album"); err == nil {
		t.Fatal("saved album fields crossed owner boundary")
	}
	if _, err = store.SelectMusicAlbumScrapedField(t.Context(), "other", "existing-album", 2, field, "reject-other-owner"); err == nil {
		t.Fatal("album selection crossed owner boundary")
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = sqlite.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	saved, err := store.MusicAlbumScrapedFields(t.Context(), "admin", "existing-album")
	if err != nil || len(saved) != 1 || saved[0].Item.Description != field.Item.Description || saved[0].CandidateToken != "" {
		t.Fatalf("persisted fields after reopen: %+v %v", saved, err)
	}
	field.Item = scraper.Item{}
	if replay, err := store.SelectMusicAlbumScrapedField(t.Context(), "admin", "existing-album", 1, field, "select-existing-album"); err != nil || replay != revision {
		t.Fatalf("replay after reopen: %d %v", replay, err)
	}
	if _, err = store.DB.ExecContext(t.Context(), "DELETE FROM music_albums WHERE id='existing-album'"); err != nil {
		t.Fatal(err)
	}
	var remaining int
	if err = store.DB.QueryRowContext(t.Context(), "SELECT count(*) FROM music_album_scraped_fields").Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("deleted album retained scraped fields: %d %v", remaining, err)
	}
}

func TestMusicAlbumScrapedFieldsSurviveRescan(t *testing.T) {
	store, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "album-rescan.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	albumName := "Tagged album"
	observation := library.Observation{Observation: pluginproto.Observation{ExternalID: "one", RootID: "root", RelativePath: "song.flac", DisplayName: "song.flac", ObservedVersion: "v1"}, Album: &albumName, Artists: []string{"Singer"}}
	scan := func(key string) {
		t.Helper()
		if _, _, err := store.Enqueue(t.Context(), "admin", "local", testkit.ScanPlan("root"), key); err != nil {
			t.Fatal(err)
		}
		job, err := store.Claim(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if err = store.Publish(t.Context(), job, testkit.ScanPublication(job, []library.Observation{observation})); err != nil {
			t.Fatal(err)
		}
		if err = store.Finish(t.Context(), job, 1, nil); err != nil {
			t.Fatal(err)
		}
	}
	scan("album-scan-before")
	page, err := store.MusicAlbums(t.Context(), "admin", music.Query{Limit: 20})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("scanned album: %+v %v", page, err)
	}
	album := page.Items[0]
	field := works.ScrapedField{Field: "albumTracks", PluginID: "fixture", CandidateToken: "preview-token", Item: scraper.Item{ID: "edition", Title: "Online album", Tracks: []scraper.Track{{Title: "Online track"}}}}
	if _, err = store.SelectMusicAlbumScrapedField(t.Context(), "admin", album.ID, album.Revision, field, "select-scanned-album"); err != nil {
		t.Fatal(err)
	}
	observation.ObservedVersion = "v2"
	scan("album-scan-after")
	page, err = store.MusicAlbums(t.Context(), "admin", music.Query{Limit: 20})
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != album.ID {
		t.Fatalf("rescan changed album identity: %+v %v", page, err)
	}
	saved, err := store.MusicAlbumScrapedFields(t.Context(), "admin", album.ID)
	if err != nil || len(saved) != 1 || len(saved[0].Item.Tracks) != 1 || saved[0].Item.Tracks[0].Title != "Online track" {
		t.Fatalf("rescan lost selected metadata: %+v %v", saved, err)
	}
}
