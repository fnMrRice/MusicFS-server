package storage_test

import (
	"database/sql"
	"errors"
	"slices"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/database/postgres/schema"
	"github.com/fnMrRice/MusicFS-server/plugins/database/postgres/state"
	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite/migrations"
	"github.com/fnMrRice/MusicFS-server/sdk/fault"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
	_ "modernc.org/sqlite"
)

func TestPostgresSchemaAndCatalogTriggers(t *testing.T) {
	options := testkit.PostgresOptions(t)
	db := testkit.PostgresDatabase(t, options)
	ctx := t.Context()
	for range 2 {
		if err := schema.Migrate(ctx, db); err != nil {
			t.Fatal(err)
		}
	}
	if err := schema.CheckVersion(ctx, db); err != nil {
		t.Fatal(err)
	}
	comparePortableColumns(t, db)
	_, err := db.ExecContext(ctx, `
		INSERT INTO tracks VALUES ('source','admin','local','external','root','file','v','asset','audio/flac',12,
			'{"title":"Track","genre":"Jazz","year":2024,"artists":["A","B","A"],"album":"Album"}');
		INSERT INTO recordings VALUES ('recording','admin',musicfs_timestamp(),musicfs_timestamp());
		INSERT INTO recording_source_links VALUES ('admin','source','recording','auto',1);
		UPDATE tracks SET document='{"title":"Renamed","artists":["B","A"],"album":"Album"}' WHERE id='source';
		INSERT INTO works(id,owner,content_type,title,description,revision,document) VALUES ('cd','admin','cd_album',
			'CD','',1,'{"externalIds":[{"namespace":"catalog_number","value":"CAT"}]}');
		INSERT INTO work_packages(id,owner,work_id,provider_id,root_id,relative_path,revision,document) VALUES
			('pkg','admin','cd','local','root','dir',1,'{"nodes":[{"kind":"audio","sourceTrackId":"source"}]}');`)
	if err != nil {
		t.Fatal(err)
	}
	var title, credits, catalog string
	err = db.QueryRowContext(ctx, `SELECT m.title,
		(SELECT string_agg(a.name,',' ORDER BY c.position) FROM music_track_artists c JOIN music_artists a ON a.id=c.artist_id WHERE c.track_id=m.id),
		b.external_id FROM music_tracks m JOIN music_album_tracks s ON s.track_id=m.id JOIN music_albums b ON b.id=s.album_id
		WHERE m.id='recording' AND b.id='cd'`).Scan(&title, &credits, &catalog)
	if err != nil || title != "Renamed" || credits != "B,A" || catalog != "CAT" {
		t.Fatalf("unexpected projected catalog: %q %q %q, %v", title, credits, catalog, err)
	}
}

// 便携备份按列复制业务记录，新增加的表和列也必须出现在另一种引擎的迁移中。
func comparePortableColumns(t *testing.T, db *sql.DB) {
	t.Helper()
	ctx := t.Context()
	source, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	source.SetMaxOpenConns(1)
	steps, err := migrations.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range steps {
		if _, err := source.ExecContext(ctx, step.SQL); err != nil {
			t.Fatal(err)
		}
	}
	columns := func(db *sql.DB, query string) []string {
		rows, err := db.QueryContext(ctx, query)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var names []string
		for rows.Next() {
			var column string
			if err := rows.Scan(&column); err != nil {
				t.Fatal(err)
			}
			names = append(names, column)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		slices.Sort(names)
		return names
	}
	want := columns(source, `SELECT s.name||'.'||c.name FROM sqlite_schema s,pragma_table_xinfo(s.name) c
		WHERE s.type='table' AND s.name NOT LIKE 'sqlite_%'`)
	got := columns(db, `SELECT c.table_name||'.'||c.column_name FROM information_schema.columns c
		JOIN information_schema.tables t USING(table_catalog,table_schema,table_name)
		WHERE c.table_schema=current_schema() AND t.table_type='BASE TABLE'
		AND c.table_name NOT IN ('postgres_schema_migrations','musicfs_plugin_values') AND c.column_name<>'rowid'`)
	if !slices.Equal(want, got) {
		for _, c := range want {
			if _, found := slices.BinarySearch(got, c); !found {
				t.Errorf("missing PostgreSQL column %s", c)
			}
		}
		for _, c := range got {
			if _, found := slices.BinarySearch(want, c); !found {
				t.Errorf("unexpected PostgreSQL column %s", c)
			}
		}
	}
}

func TestPostgresPrivateStatePreloadAndConcurrentWriters(t *testing.T) {
	options := testkit.PostgresOptions(t)
	db := testkit.PostgresDatabase(t, options)
	ctx := t.Context()
	const first = "b1548e96-3081-5c9c-8376-bf290e369d51"
	const second = "16b003e5-1bab-52bb-be10-0e83ec1c40ea"
	initial, err := state.Read(ctx, db, "plugin", first, "settings")
	if err != nil || initial.Revision != 0 {
		t.Fatalf("unexpected initial state: %#v, %v", initial, err)
	}
	var absent bool
	if err := db.QueryRowContext(ctx, "SELECT to_regclass('musicfs_plugin_values') IS NULL").Scan(&absent); err != nil || !absent {
		t.Fatalf("preload must leave the database untouched: %v", err)
	}
	results := make(chan error, 2)
	for range 2 {
		pool := testkit.PostgresDatabase(t, options)
		go func() {
			s, err := state.Open(ctx, pool, "plugin", first)
			if err == nil {
				defer s.Close()
				_, err = s.CompareAndSet(ctx, pluginproto.StateRequest{Key: "settings", Value: []byte("null")})
			}
			results <- err
		}()
	}
	success, conflicts := 0, 0
	for range 2 {
		err := <-results
		var failure *fault.Error
		switch {
		case err == nil:
			success++
		case errors.As(err, &failure) && failure.Code == "PRECONDITION_FAILED":
			conflicts++
		default:
			t.Fatal(err)
		}
	}
	if success != 1 || conflicts != 1 {
		t.Fatalf("concurrent writes: %d succeeded, %d conflicted", success, conflicts)
	}
	for _, scope := range []struct {
		plugin, instance string
		want             int64
	}{{"plugin", first, 1}, {"plugin", second, 0}, {"another", first, 0}} {
		got, err := state.Read(ctx, db, scope.plugin, scope.instance, "settings")
		if err != nil || got.Revision != scope.want || (scope.want == 1 && string(got.Value) != "null") {
			t.Fatalf("unexpected scope state: %#v, %v", got, err)
		}
	}
}
