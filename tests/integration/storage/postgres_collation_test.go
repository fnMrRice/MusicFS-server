package storage_test

import (
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/library/music"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugindb"
	"github.com/fnMrRice/MusicFS-server/src/storage"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestPostgresICUMusicOrderingAndCursor(t *testing.T) {
	options := testkit.PostgresICUOptions(t)
	ctx := t.Context()
	root := t.TempDir()
	store, err := storage.Open(ctx, "postgres", storage.Options{DataRoot: root, Values: options})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	state, err := plugindb.OpenBackend(ctx, plugindb.BackendConfig{Name: "postgres", DataRoot: root, Options: options}, "", "musicfs.plugin.test", "state")
	if err != nil {
		t.Fatal(err)
	}
	defer state.Close()
	db := testkit.PostgresDatabase(t, options)
	var foreignOrder bool
	if err := db.QueryRowContext(ctx, `SELECT 'a'::text < 'B'::text`).Scan(&foreignOrder); err != nil || !foreignOrder {
		t.Fatalf("expected ICU default collation: %v, %v", foreignOrder, err)
	}
	var otherCollations int
	err = db.QueryRowContext(ctx, `SELECT count(*) FROM pg_attribute a JOIN pg_class c ON c.oid=a.attrelid
		JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema() AND c.relkind='r'
		AND NOT a.attisdropped AND a.atttypid='text'::regtype AND a.attcollation<>'"C"'::regcollation`).Scan(&otherCollations)
	if err != nil || otherCollations != 0 {
		t.Fatalf("unexpected text column collations: %d, %v", otherCollations, err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO music_tracks(id,owner,title,genre) VALUES
		('a','admin','Beyoncé','rock'),('B','admin','Beyoncé','Rock')`); err != nil {
		t.Fatal(err)
	}
	page, err := store.MusicTracks(ctx, "admin", music.Query{Limit: 1, Q: "beyoncé"})
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != "B" || page.NextAfter == nil {
		t.Fatalf("first page: %+v, %v", page, err)
	}
	page, err = store.MusicTracks(ctx, "admin", music.Query{Limit: 1, Q: "beyoncé", After: *page.NextAfter})
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != "a" || page.NextAfter != nil {
		t.Fatalf("next page: %+v, %v", page, err)
	}
	page, err = store.MusicTracks(ctx, "admin", music.Query{Limit: 1, Q: "BEYONCÉ"})
	if err != nil || len(page.Items) != 0 {
		t.Fatalf("accented search: %+v, %v", page, err)
	}
	genres, err := store.MusicGenres(ctx, "admin", music.Query{Limit: 10})
	if err != nil || len(genres.Items) != 1 || genres.Items[0].Name != "Rock" || genres.Items[0].TrackCount != 2 {
		t.Fatalf("genre grouping: %+v, %v", genres, err)
	}
}
