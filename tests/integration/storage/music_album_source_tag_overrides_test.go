package storage_test

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"slices"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/database/postgres/schema"
	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite/migrations"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/storage"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestMusicAlbumSourceTagOverridesPreinstalledUpgrade(t *testing.T) {
	for _, version := range []int{51, 52} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "preinstalled.sqlite")
			db := openMigrationFixture(t, path)
			exec := func(query string, args ...any) {
				t.Helper()
				if _, err := db.Exec(query, args...); err != nil {
					t.Fatal(err)
				}
			}
			steps, err := migrations.Load()
			if err != nil {
				t.Fatal(err)
			}
			exec("CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY)")
			for _, step := range steps {
				if step.Version > 53 {
					break
				}
				exec(step.SQL)
				if step.Version <= version {
					exec("INSERT INTO schema_migrations VALUES(?)", step.Version)
				}
			}
			exec("INSERT INTO tracks(id,owner,provider,external_id,root_id,path,version,asset_id,mime,size,document) VALUES('song','admin','library','ext','r','s','v','a','audio/flac',1,'{}')")
			exec("INSERT INTO music_albums(id,owner,name) VALUES('album','admin','Selected')")
			exec("INSERT INTO music_album_source_tag_overrides VALUES('admin','song','Common','album')")
			if err = db.Close(); err != nil {
				t.Fatal(err)
			}
			s, err := sqlite.Open(t.Context(), path)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			if _, err = s.DB.Exec("UPDATE tracks SET document=? WHERE id='song'", `{"album":"Common","title":"Song"}`); err != nil {
				t.Fatal(err)
			}
			var id string
			if err = s.DB.QueryRow("SELECT album_id FROM music_album_source_refs WHERE source_id='song' AND origin_id='tag'").Scan(&id); err != nil || id != "album" {
				t.Fatalf("Preinstalled source override lost on upgrade: %q %v", id, err)
			}
		})
	}
}

func TestMusicAlbumSourceTagOverridesSurviveRescan(t *testing.T) {
	testAlbumBackends(t, checkAlbumSourceTagOverrides)
}

func TestPostgresSourceTagOverridePreinstalledUpgrade(t *testing.T) {
	db := testkit.PostgresDatabase(t, testkit.PostgresOptions(t))
	ctx := t.Context()
	if err := schema.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	// 还原为已经预装 004、版本登记仍为 003 的状态。
	if _, err := db.ExecContext(ctx, `DELETE FROM postgres_schema_migrations WHERE version>=4;
		INSERT INTO tracks(id,owner,provider,external_id,root_id,path,version,asset_id,mime,size,document)
		VALUES('song','admin','library','ext','r','s','v','a','audio/flac',1,'{}');
		INSERT INTO music_albums(id,owner,name) VALUES('album','admin','Selected');
		INSERT INTO music_album_source_tag_overrides VALUES('admin','song','Common','album');`); err != nil {
		t.Fatal(err)
	}
	if err := schema.Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE tracks SET document='{"album":"Common","title":"Song"}' WHERE id='song'`); err != nil {
		t.Fatal(err)
	}
	var album string
	if err := db.QueryRowContext(ctx, "SELECT album_id FROM music_album_source_refs WHERE source_id='song' AND origin_id='tag'").Scan(&album); err != nil || album != "album" {
		t.Fatalf("Preinstalled source override lost on upgrade: %q %v", album, err)
	}
}

func checkAlbumSourceTagOverrides(t *testing.T, store storage.Backend, db *sql.DB) {
	t.Helper()
	ctx := t.Context()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, query, args...); err != nil {
			t.Fatalf("Executing %s: %v", query, err)
		}
	}
	scalar := func(query string, args ...any) string {
		t.Helper()
		var value string
		if err := db.QueryRowContext(ctx, query, args...).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	const provider = "one-library"
	const shared = "Shared"
	run := 0
	scan := func(tag string) {
		t.Helper()
		run++
		input := []library.Observation{}
		for i, album := range []string{tag, shared} {
			song := fmt.Sprintf("song-%d", i)
			title := fmt.Sprintf("Song %d, scan %d", i, run)
			input = append(input, library.Observation{Observation: pluginproto.Observation{
				ExternalID: song, RootID: "root", RelativePath: song + ".flac", DisplayName: song, ObservedVersion: fmt.Sprintf("v%d", run),
			}, Album: &album, Title: &title, Artists: []string{"Singer"}})
		}
		if _, _, err := store.Enqueue(ctx, "admin", provider, testkit.ScanPlan("root"), fmt.Sprintf("scan-group-%d", run)); err != nil {
			t.Fatal(err)
		}
		job, err := store.Claim(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err = store.Publish(ctx, job, testkit.ScanPublication(job, input)); err != nil {
			t.Fatal(err)
		}
		if err = store.Finish(ctx, job, len(input), nil); err != nil {
			t.Fatal(err)
		}
	}
	const membership = "SELECT album_id FROM music_album_source_refs WHERE origin_id='tag' AND source_id=$1"
	const put = "INSERT INTO music_album_source_tag_overrides(owner,source_id,tag_name,album_id) VALUES($1,$2,$3,$4)"
	scan(shared)
	source := scalar("SELECT id FROM tracks WHERE owner='admin' AND external_id='song-0'")
	other := scalar("SELECT id FROM tracks WHERE owner='admin' AND external_id='song-1'")
	parent := scalar(membership, other)
	const chosen = "source-target"
	exec("INSERT INTO music_albums(id,owner,name,origin,tag_provider,tag_name) VALUES($1,'admin',$2,'tag','another-provider',$2)", chosen, shared)
	exec("INSERT INTO music_album_tag_aliases(owner,provider,tag_name,album_id) VALUES('admin',$1,$2,$3)", provider, shared, parent)
	exec(put, "admin", source, shared, chosen)
	scan(shared)
	if scalar(membership, source) != chosen || scalar(membership, other) != parent {
		t.Fatal("Source override did not take precedence independently for two same-tag songs")
	}

	// 保留两张已明确归属的同名发行；手工交叉关联让同一曲目可以展示双专辑。
	track := scalar("SELECT track_id FROM music_sources WHERE id=$1", source)
	exec("INSERT INTO music_manual_album_tracks(album_id,track_id,position) VALUES($1,$2,0)", parent, track)
	detail, err := store.MusicTrack(ctx, "admin", track)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(detail.AlbumIDs, chosen) || !slices.Contains(detail.AlbumIDs, parent) || len(detail.Albums) != 2 {
		t.Fatalf("Explicit same-name albums lost from track detail: %+v", detail.AlbumIDs)
	}
	scan("Changed")
	if got := scalar(membership, source); got == chosen || got == parent {
		t.Fatal("Source kept the old release after its tag changed")
	}
	scan(shared)
	if scalar(membership, source) != chosen || scalar(membership, other) != parent {
		t.Fatal("Returning source tag lost its stored override")
	}
	exec(put, "admin", source, "Alternate label", chosen)
	scan("Alternate label")
	if scalar(membership, source) != chosen || scalar("SELECT count(*) FROM music_albums WHERE tag_provider=$1 AND tag_name='Alternate label'", provider) != "0" {
		t.Fatal("A source-only assignment created an empty tag album")
	}

	exec("INSERT INTO music_albums(id,owner,name) VALUES('foreign-target','another-owner','Other')")
	for _, args := range [][]any{{"admin", source, "Foreign", "foreign-target"}, {"another-owner", source, "Foreign", "foreign-target"}} {
		if _, err := db.ExecContext(ctx, put, args...); err == nil {
			t.Fatal("Cross-owner source override accepted")
		}
	}
	exec("DELETE FROM music_albums WHERE id=$1", chosen)
	if scalar("SELECT count(*) FROM music_album_source_tag_overrides WHERE source_id=$1", source) != "0" {
		t.Fatal("Deleting destination retained its source override")
	}
	// 实际扫描项还可能被录音、媒体或自动化任务独立保护；仅使用无其它依赖的音源验证删除级联。
	const disposable = "unused-source"
	exec("INSERT INTO tracks(id,owner,provider,external_id,root_id,path,version,asset_id,mime,size,document) VALUES($1,'admin','free','external','r','s','v','asset','audio/flac',1,'{}')", disposable)
	exec(put, "admin", disposable, shared, parent)
	exec("DELETE FROM tracks WHERE id=$1", disposable)
	if scalar("SELECT count(*) FROM music_album_source_tag_overrides WHERE source_id=$1", disposable) != "0" {
		t.Fatal("Deleting source retained its album override")
	}
}
