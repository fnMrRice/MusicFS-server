package storage_test

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite/migrations"
)

func TestMusicCatalogAndPlaylistUpgradePreservesReferences(t *testing.T) {
	path := filepath.Join(t.TempDir(), "library.sqlite")
	ctx := t.Context()
	db := openMigrationFixture(t, path)
	exec := func(statement string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, statement, args...); err != nil {
			t.Fatal(statement, err)
		}
	}
	exec("CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY)")
	steps, err := migrations.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range steps {
		if step.Version > 36 {
			break
		}
		exec(step.SQL)
		exec("INSERT INTO schema_migrations VALUES(?)", step.Version)
	}
	exec(`INSERT INTO tracks(id,owner,provider,external_id,root_id,path,version,asset_id,mime,size,document)
	 VALUES('src','admin','instance','provider-key','root','a.flac','v1','asset','audio/flac',12,
	 '{"id":"src","title":"Original","artists":["Second","First","Second"],"album":"Tagged","genre":"Jazz","year":2008}')`)
	exec(`INSERT INTO source_assets(id,owner,track_id,version,current,document,created_at)
	 VALUES('asset','admin','src','v1',1,'{}','2020-04-01')`)
	exec(`INSERT INTO jobs(id,owner,target,kind,generation,state,created_at,updated_at,plan,plan_digest)
	 VALUES('scan','admin','instance','scan',1,'succeeded','2020','2020','{"pluginId":"musicfs.localfiles"}','hash'),
	 ('rescan','admin','instance','scan',2,'succeeded','2021','2021','{"pluginId":"musicfs.plugin.source.localfiles"}','hash2')`)
	exec(`INSERT INTO works(id,owner,content_type,title,description,revision,document)
	 VALUES('cd','admin','cd_album','Compact Disc','',1,'{"externalIds":[{"namespace":"catalog_number","value":"CD-1"}]}')`)
	exec(`INSERT INTO work_packages(id,owner,work_id,provider_id,root_id,relative_path,revision,document,kind)
	 VALUES('disc','admin','cd','instance','root','disc',1,'{"nodes":[{"kind":"audio","sourceTrackId":"src"}]}','cd_dump')`)
	exec(`INSERT INTO personal_playlists VALUES('list','admin',1,1,0,
	 '{"bindings":[{"bindingId":"binding","kind":"local","entries":[{"entryId":"entry","source":{"sourceTrackId":"src"}}]}],"ruleRevision":1}')`)
	exec(`INSERT INTO playlist_hidden_rules VALUES('hide','admin','list',1,
	 '{"Rule":{"source":{"sourceTrackId":"src"},"scope":"allSources","active":true},"Allowed":[{"sourceTrackId":"src"}]}')`)
	exec("INSERT INTO personal_recently_played(owner,source_track_id,played_at) VALUES('admin','src','2021-02-03T12:00:00Z')")
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := sqlite.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	db = store.DB
	value := func(query, want string) {
		t.Helper()
		var actual sql.NullString
		if err := db.QueryRowContext(ctx, query).Scan(&actual); err != nil || !actual.Valid || actual.String != want {
			t.Fatalf("%s returned %v, want %q: %v", query, actual, want, err)
		}
	}
	value("SELECT title||'|'||genre||'|'||year FROM music_tracks WHERE id='src'", "Original|Jazz|2008")
	value(`SELECT group_concat(name,'|') FROM (SELECT name FROM music_track_artists r JOIN music_artists a ON a.id=r.artist_id ORDER BY position)`, "Second|First")
	value("SELECT plugin_id||'|'||provider_instance_id||'|'||external_id FROM music_sources", "musicfs.plugin.source.localfiles|instance|provider-key")
	value("SELECT external_id||'|'||is_external FROM music_albums WHERE id='cd'", "CD-1|1")
	value("SELECT count(*) FROM music_playlist_entries WHERE playlist_id='recently-played'", "0")
	value("SELECT type_id||'|'||work_id||'|'||source_id||'|'||recorded_at FROM content_history_pending", "cd_album|cd|src|2021-02-03T12:00:00Z")
	value("SELECT count(*) FROM music_album_tracks WHERE track_id='src'", "2")
	value("SELECT created_at||'|'||updated_at FROM music_sources", "2020-04-01T00:00:00.000Z|2020-04-01T00:00:00.000Z")
	value("SELECT track_id||'|'||forced_source_id FROM music_playlist_entries WHERE entry_id='entry'", "src|src")
	value("SELECT json_extract(document,'$.Allowed[0].forcedSourceTrackId') FROM playlist_hidden_rules", "src")
	value("SELECT rule_revision FROM personal_playlists", "2")
	exec("UPDATE tracks SET version=version,document=document WHERE id='src'")
	value("SELECT updated_at FROM music_sources", "2020-04-01T00:00:00.000Z")
	exec("UPDATE tracks SET version='v2' WHERE id='src'")
	value("SELECT updated_at<>created_at FROM music_sources", "1")
	value("SELECT updated_at FROM music_tracks", "2020-04-01T00:00:00.000Z")
	if _, err = db.ExecContext(ctx, "UPDATE music_tracks SET rating=2.5"); err == nil {
		t.Fatal("Accepted fractional star rating")
	}
	exec("UPDATE music_tracks SET title='Curated single',rating=3,metadata_locked=1,revision=revision+1 WHERE id='src'")
	exec(`INSERT INTO recordings VALUES('logical','admin','2021-01-01T00:00:00Z','2021-01-01T00:00:00Z')`)
	exec(`INSERT INTO recording_source_links VALUES('admin','src','logical','manual',1)`)
	value("SELECT track_id FROM music_playlist_entries WHERE entry_id='entry'", "logical")
	value("SELECT json_extract(document,'$.Allowed[0].trackId') FROM playlist_hidden_rules", "logical")
	value("SELECT count(*) FROM music_album_tracks WHERE track_id='logical'", "2")
	value("SELECT title||'|'||rating FROM music_tracks WHERE id='logical'", "Curated single|3")
	exec("UPDATE music_tracks SET title='Curated',metadata_locked=1,rating=4,revision=revision+1 WHERE id='logical'")
	exec("UPDATE tracks SET document=json_set(document,'$.title','Changed tag','$.artists',json_array('New')) WHERE id='src'")
	value("SELECT title||'|'||rating FROM music_tracks WHERE id='logical'", "Curated|4")
	value("SELECT count(*) FROM music_track_artists WHERE track_id='logical'", "2")
	exec("UPDATE recording_source_links SET recording_id=NULL WHERE owner='admin' AND source_id='src'")
	value("SELECT track_id||'|'||forced_source_id FROM music_playlist_entries WHERE entry_id='entry'", "logical|src")
	value("SELECT track_id FROM music_sources WHERE id='src'", "src")
	exec("UPDATE tracks SET provider='instance-renamed' WHERE id='src'")
	value("SELECT plugin_id||'|'||provider_instance_id FROM music_sources", "musicfs.plugin.source.localfiles|instance-renamed")
	value("SELECT count(*) FROM music_albums WHERE name='Tagged'", "1")
	value("SELECT tag_provider FROM music_albums WHERE name='Tagged'", "instance-renamed")
}
