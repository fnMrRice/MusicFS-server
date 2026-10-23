package storage_test

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite/migrations"
)

type artistMigrationTrack struct {
	id       string
	document string
	artists  []string
}

func TestMultiArtistMigrationPreservesTrackIdentityRelationsAndSnapshots(t *testing.T) {
	path := filepath.Join(t.TempDir(), "multi-artists.sqlite")
	db, latest := prepareArtistMigration(t, path)
	spaces := "\t\n\v\f\r \u0085\u00a0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200a\u2028\u2029\u202f\u205f\u3000"
	fixtures := []artistMigrationTrack{
		{"split", `{"id":"split","title":"Song / title","artists":["Newbiao好梦一场 / 洛天依 Official/星尘infinity","星尘infinity/Newbiao好梦一场"],"source":{"id":"original/source"},"extra":[3,null,{"name":"A/B"}]}`, []string{"Newbiao好梦一场", "洛天依 Official", "星尘infinity"}},
		{"order", `{"artists":[" B / A / B ","C / A","b","A／D"]}`, []string{"B", "A", "C", "b", "A／D"}},
		{"empty", `{"artists":[" / / ","", "\t"]}`, []string{}},
		{"space", artistMigrationDocument(t, []string{spaces + "A" + spaces + "/" + spaces + "B" + spaces, "A"}), []string{"A", "B"}},
		{"nul", `{"artists":[" A\u0000B / C ","A\u0000B","/ \u0000 /","A\u0000C"]}`, []string{"A\x00B", "C", "\x00", "A\x00C"}},
	}
	fixtures = append(fixtures, unchangedArtistMigrationTracks()...)
	seedArtistMigrationTracks(t, db, fixtures)
	for _, statement := range []string{
		`INSERT INTO source_assets(id,owner,track_id,version,current,document,created_at) VALUES('asset-split','admin','split','v1',1,'{"artists":["A/B"],"source":"unchanged"}','2026-01-01')`,
		`INSERT INTO recordings VALUES('recording','admin','2026-01-01','2026-01-01')`,
		`INSERT INTO recording_source_links VALUES('admin','split','recording','manual',7)`,
		`INSERT INTO matching_revisions VALUES('admin',7)`,
		`INSERT INTO library_snapshots VALUES('snapshot','admin','local','41','2099-01-01')`,
		`INSERT INTO library_snapshot_items SELECT 'snapshot',0,document FROM tracks WHERE id='split'`,
		`INSERT INTO library_cursors VALUES('cursor','snapshot',0)`,
		`INSERT INTO catalog_queries VALUES('query','admin','{"artists":["A/B"]}','41',1,'2099-01-01',0,'query-key')`,
		`INSERT INTO catalog_query_items SELECT 'query',0,document FROM tracks WHERE id='split'`,
		`INSERT INTO catalog_query_cursors VALUES('query-cursor','query',0)`,
		`INSERT INTO remote_sources VALUES('remote','admin','provider','account','external','{"artists":["A/B"]}')`,
	} {
		if _, err := db.ExecContext(t.Context(), statement); err != nil {
			t.Fatal(err)
		}
	}
	preserved := captureArtistMigrationRelations(t, db)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	current, err := sqlite.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { current.Close() })
	assertArtistMigrationTracks(t, current.DB, fixtures, true)
	assertArtistMigrationVersion(t, current.DB, latest, 42)
	if got := captureArtistMigrationRelations(t, current.DB); !reflect.DeepEqual(got, preserved) {
		t.Fatalf("migration changed track identity, relations or snapshots: got %v, want %v", got, preserved)
	}
	wantBackups := make([]string, 0, latest-34)
	for version := 35; version <= latest; version++ {
		wantBackups = append(wantBackups, fmt.Sprintf("%s.before-%03d", path, version))
	}
	assertMigrationBackups(t, path, wantBackups)
	backup := openMigrationFixture(t, path+".before-035")
	assertArtistMigrationVersion(t, backup, 34, 41)
	assertArtistMigrationTracks(t, backup, fixtures, false)
	if got := captureArtistMigrationRelations(t, backup); !reflect.DeepEqual(got, preserved) {
		t.Fatal("migration backup lost original relations or snapshots")
	}
	if err := backup.Close(); err != nil {
		t.Fatal(err)
	}
	if err := current.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := sqlite.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	assertArtistMigrationTracks(t, reopened.DB, fixtures, true)
	assertArtistMigrationVersion(t, reopened.DB, latest, 42)
	assertMigrationBackups(t, path, wantBackups)
	if got := captureArtistMigrationRelations(t, reopened.DB); !reflect.DeepEqual(got, preserved) {
		t.Fatal("reopening changed preserved relations or snapshots")
	}
}

func TestMultiArtistMigrationKeepsRevisionWithoutChanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "unchanged-artists.sqlite")
	db, latest := prepareArtistMigration(t, path)
	fixtures := unchangedArtistMigrationTracks()
	seedArtistMigrationTracks(t, db, fixtures)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	current, err := sqlite.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer current.Close()
	assertArtistMigrationTracks(t, current.DB, fixtures, true)
	assertArtistMigrationVersion(t, current.DB, latest, 41)
}

func unchangedArtistMigrationTracks() []artistMigrationTrack {
	return []artistMigrationTrack{
		{"normalized", "{ \"artists\": [\"\\u0041\", \"B\", \"a\", \"AC／DC\", \"\\u200bC\\ufeff\"] }", nil},
		{"empty-array", `{"artists":[]}`, nil},
		{"missing", `{"title":"A/B"}`, nil},
		{"null", `{"artists":null}`, nil},
		{"scalar", `{"artists":"A/B"}`, nil},
		{"object", `{"artists":{"name":"A/B"}}`, nil},
		{"mixed", `{"artists":["A/B",12,null,{"name":"C/D"},["E/F"],true]}`, nil},
		{"malformed", `{"artists":["A/B"`, nil},
	}
}

func artistMigrationDocument(t *testing.T, artists []string) string {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"artists": artists})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func prepareArtistMigration(t *testing.T, path string) (*sql.DB, int) {
	t.Helper()
	db := openMigrationFixture(t, path)
	db.SetMaxOpenConns(1)
	steps, err := migrations.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(t.Context(), "CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	for _, step := range steps {
		if step.Version > 34 {
			break
		}
		if _, err = db.ExecContext(t.Context(), step.SQL); err != nil {
			t.Fatal(step.Version, err)
		}
		if _, err = db.ExecContext(t.Context(), "INSERT INTO schema_migrations VALUES(?)", step.Version); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = db.ExecContext(t.Context(), "UPDATE library_revision SET revision=41"); err != nil {
		t.Fatal(err)
	}
	return db, steps[len(steps)-1].Version
}

func seedArtistMigrationTracks(t *testing.T, db *sql.DB, tracks []artistMigrationTrack) {
	t.Helper()
	for _, track := range tracks {
		if _, err := db.ExecContext(t.Context(), `INSERT INTO tracks
			(id,owner,provider,external_id,root_id,path,version,asset_id,mime,size,document)
			VALUES(?,'admin','local',?,'music',?,'v1',?,'audio/flac',100,?)`,
			track.id, track.id, track.id+".flac", "asset-"+track.id, []byte(track.document)); err != nil {
			t.Fatal(err)
		}
	}
}

func assertArtistMigrationTracks(t *testing.T, db *sql.DB, tracks []artistMigrationTrack, migrated bool) {
	t.Helper()
	for _, track := range tracks {
		var document string
		if err := db.QueryRowContext(t.Context(), "SELECT document FROM tracks WHERE id=?", track.id).Scan(&document); err != nil {
			t.Fatal(err)
		}
		if !migrated || track.artists == nil {
			if document != track.document {
				t.Fatalf("unchanged document %s was modified: %s", track.id, document)
			}
			continue
		}
		var got, want map[string]any
		if err := json.Unmarshal([]byte(document), &got); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal([]byte(track.document), &want); err != nil {
			t.Fatal(err)
		}
		var artists struct {
			Artists []string `json:"artists"`
		}
		if err := json.Unmarshal([]byte(document), &artists); err != nil || !reflect.DeepEqual(artists.Artists, track.artists) {
			t.Fatalf("unexpected migrated artists for %s: got %v, want %v, err %v", track.id, artists.Artists, track.artists, err)
		}
		delete(got, "artists")
		delete(want, "artists")
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("migration changed unrelated metadata for %s: got %v, want %v", track.id, got, want)
		}
	}
}

func assertArtistMigrationVersion(t *testing.T, db *sql.DB, version, revision int) {
	t.Helper()
	var gotVersion, gotRevision int
	if err := db.QueryRowContext(t.Context(), "SELECT MAX(version) FROM schema_migrations").Scan(&gotVersion); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(t.Context(), "SELECT revision FROM library_revision").Scan(&gotRevision); err != nil {
		t.Fatal(err)
	}
	if gotVersion != version || gotRevision != revision {
		t.Fatalf("unexpected database version/revision: got %d/%d, want %d/%d", gotVersion, gotRevision, version, revision)
	}
}

func captureArtistMigrationRelations(t *testing.T, db *sql.DB) map[string][][]any {
	t.Helper()
	queries := map[string]string{
		"tracks": "SELECT id,owner,provider,external_id,root_id,path,version,asset_id,mime,size FROM tracks ORDER BY id",
	}
	for _, table := range []string{"source_assets", "recordings", "recording_source_links", "matching_revisions", "library_snapshots", "library_snapshot_items", "library_cursors", "catalog_queries", "catalog_query_items", "catalog_query_cursors", "remote_sources"} {
		queries[table] = "SELECT * FROM " + table
	}
	result := make(map[string][][]any)
	for name, query := range queries {
		rows, err := db.QueryContext(t.Context(), query)
		if err != nil {
			t.Fatal(err)
		}
		columns, err := rows.Columns()
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			values, pointers := make([]any, len(columns)), make([]any, len(columns))
			for i := range values {
				pointers[i] = &values[i]
			}
			if err := rows.Scan(pointers...); err != nil {
				t.Fatal(err)
			}
			result[name] = append(result[name], values)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
	}
	return result
}
