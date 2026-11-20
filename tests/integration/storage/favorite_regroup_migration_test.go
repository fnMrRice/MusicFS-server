package storage_test

import (
	"path/filepath"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite/migrations"
	"github.com/fnMrRice/MusicFS-server/src/library/personal"
	"github.com/fnMrRice/MusicFS-server/src/metadata/matching"
)

func TestFavoriteRegroupMigrationUpgradesExistingCollections(t *testing.T) {
	path := filepath.Join(t.TempDir(), "favorites-v38.sqlite")
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
		if step.Version > 38 {
			break
		}
		exec(step.SQL)
		exec("INSERT INTO schema_migrations VALUES(?)", step.Version)
	}
	for _, id := range []string{"source-a", "source-b"} {
		stamp := "2020-04-01T00:00:00Z"
		if id == "source-b" {
			stamp = "2021-05-02T00:00:00Z"
		}
		exec(`INSERT INTO tracks(id,owner,provider,external_id,root_id,path,version,asset_id,mime,size,document)
 VALUES(?,'admin','local',?,'root',?,'v1',?,'audio/flac',12,
 json_object('id',?,'providerInstanceId','local','title',?,'artists',json_array(),'availability','available'))`,
			id, id, id+".flac", "asset-"+id, id, id)
		exec(`INSERT INTO music_playlist_entries(owner,playlist_id,binding_id,entry_id,track_id,forced_source_id,position,created_at,updated_at)
 VALUES('admin','favorites','',?,?,?,0,?,?)`, id, id, id, stamp, stamp)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := sqlite.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var version int
	if err = store.DB.QueryRowContext(t.Context(), "SELECT MAX(version) FROM schema_migrations").Scan(&version); err != nil || version != 39 {
		t.Fatalf("favorite migration was not applied: %d %v", version, err)
	}
	_, err = store.ManualLink(t.Context(), "admin", matching.LinkRequest{SourceTrackIDs: []string{"source-a", "source-b"}}, "upgraded-favorites")
	if err != nil {
		t.Fatal(err)
	}
	page, err := store.PersonalCollection(t.Context(), "admin", personal.Favorites, "", 10)
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("upgraded favorites were not merged: %+v %v", page, err)
	}
	entry := page.Items[0]
	if entry.ForcedSourceID == nil || *entry.ForcedSourceID != "source-a" || entry.RecordedAt.Format("2006-01-02") != "2020-04-01" || len(entry.MusicTrack.Sources) != 2 {
		t.Fatalf("existing favorite details were not preserved: %+v", entry)
	}
}
