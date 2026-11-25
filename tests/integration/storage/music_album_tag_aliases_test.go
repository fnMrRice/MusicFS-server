package storage_test

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "github.com/fnMrRice/MusicFS-server/plugins/database/postgres"
	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite/migrations"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/library/music"
	"github.com/fnMrRice/MusicFS-server/src/storage"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestMusicAlbumTagAliasesPreservedOnSchemaUpgrade(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "preinstalled.sqlite")
	db := openMigrationFixture(t, path)
	steps, err := migrations.Load()
	if err != nil {
		t.Fatal(err)
	}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec("CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY)")
	for _, step := range steps {
		if step.Version > 51 {
			break
		}
		exec(step.SQL)
		exec("INSERT INTO schema_migrations VALUES(?)", step.Version)
	}
	// 在旧版上预装相同 SQL。正式升级时须保留此期间写入的来源映射。
	exec(steps[51].SQL)
	exec("INSERT INTO music_albums(id,owner,name) VALUES('canonical','admin','Album')")
	exec("INSERT INTO music_album_tag_aliases VALUES('admin','local','Orig','canonical')")
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := sqlite.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var id string
	if err := s.DB.QueryRowContext(ctx, "SELECT album_id FROM music_album_tag_aliases WHERE owner='admin' AND provider='local' AND tag_name='Orig'").Scan(&id); err != nil || id != "canonical" {
		t.Fatalf("Missing preinstalled tag alias after upgrade: %q, %v", id, err)
	}
}

func TestMusicAlbumTagAliasesSurviveRescan(t *testing.T) {
	testAlbumBackends(t, checkAlbumTagAliases)
}

func testAlbumBackends(t *testing.T, check func(*testing.T, storage.Backend, *sql.DB)) {
	t.Run("sqlite", func(t *testing.T) {
		s, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "aliases.sqlite"))
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		check(t, s, s.DB)
	})
	t.Run("postgres", func(t *testing.T) {
		options := testkit.PostgresOptions(t)
		s, err := storage.Open(t.Context(), "postgres", storage.Options{DataRoot: t.TempDir(), Values: options})
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		check(t, s, testkit.PostgresDatabase(t, options))
	})
}

func checkAlbumTagAliases(t *testing.T, store storage.Backend, db *sql.DB) {
	t.Helper()
	ctx := t.Context()
	scan := func(provider, albumName, version, key string) {
		t.Helper()
		obs := library.Observation{Observation: pluginproto.Observation{ExternalID: "song", RootID: "root",
			RelativePath: "song.flac", DisplayName: "song.flac", ObservedVersion: version},
			Album: &albumName, Artists: []string{"Singer"}}
		if _, _, err := store.Enqueue(ctx, "admin", provider, testkit.ScanPlan("root"), key); err != nil {
			t.Fatal(err)
		}
		job, err := store.Claim(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err = store.Publish(ctx, job, testkit.ScanPublication(job, []library.Observation{obs})); err != nil {
			t.Fatal(err)
		}
		if err = store.Finish(ctx, job, 1, nil); err != nil {
			t.Fatal(err)
		}
	}
	albumFor := func(provider string) string {
		t.Helper()
		page, err := store.MusicAlbums(ctx, "admin", music.Query{Limit: 10, ProviderID: provider})
		if err != nil || len(page.Items) != 1 {
			t.Fatalf("Album for provider %q: %+v, %v", provider, page, err)
		}
		return page.Items[0].ID
	}
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	scan("primary", "Main name", "v1", "scan-primary")
	scan("secondary", "Source spelling", "v1", "scan-secondary")
	scan("third", "Source spelling", "v1", "scan-third")
	primary, old, independent := albumFor("primary"), albumFor("secondary"), albumFor("third")
	if primary == old || independent == old {
		t.Fatal("Independent source albums should initially have distinct identities")
	}
	exec("INSERT INTO music_album_tag_aliases(owner,provider,tag_name,album_id) VALUES('admin','secondary','Source spelling',$1)", primary)
	exec("UPDATE music_album_source_refs SET album_id=$1 WHERE album_id=$2", primary, old)
	exec("DELETE FROM music_albums WHERE id=$1", old)
	scan("secondary", "Different release", "v2", "scan-different")
	if albumFor("secondary") == primary {
		t.Fatal("A different release used the old tag mapping")
	}
	scan("secondary", "Source spelling", "v3", "scan-return")
	if albumFor("secondary") != primary || albumFor("third") != independent {
		t.Fatal("Original source spelling was not mapped independently per provider")
	}
	exec("UPDATE tracks SET provider='renamed' WHERE owner='admin' AND provider='secondary'")
	if albumFor("renamed") != primary {
		t.Fatal("Provider rename lost the merged album")
	}
	var duplicate int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM music_albums WHERE owner='admin' AND tag_provider='renamed' AND tag_name='Source spelling'").Scan(&duplicate); err != nil || duplicate != 0 {
		t.Fatalf("A rescan recreated %d duplicate source albums: %v", duplicate, err)
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO music_album_tag_aliases VALUES('other','secondary','Source spelling',$1)", primary); err == nil {
		t.Fatal("A source tag could point to another owner's album")
	}
}
