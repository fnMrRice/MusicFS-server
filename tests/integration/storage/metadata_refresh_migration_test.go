package storage_test

import (
	"database/sql"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite/migrations"
)

func TestMetadataRefreshMigrationPreservesTriggersActionsAndConstraints(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "before-refresh.sqlite")
	db := openMigrationFixture(t, filename)
	steps, err := migrations.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(t.Context(), "CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	for _, step := range steps {
		if step.Version > 31 {
			break
		}
		if _, err = db.ExecContext(t.Context(), step.SQL); err != nil {
			t.Fatal(step.Version, err)
		}
		if _, err = db.ExecContext(t.Context(), "INSERT INTO schema_migrations VALUES(?)", step.Version); err != nil {
			t.Fatal(err)
		}
	}
	for _, statement := range []string{
		`INSERT INTO jobs(id,owner,target,kind,generation,state,created_at,updated_at,plan,plan_digest) VALUES('scan','admin','local','scan',1,'succeeded','2026-10-23T00:00:00Z','2026-10-23T00:00:00Z','{}','digest')`,
		`INSERT INTO tracks(id,owner,provider,external_id,root_id,path,version,asset_id,mime,size,document) VALUES('track','admin','local','song','music','song.mp3','v1','asset','audio/mpeg',100,'{}')`,
		`INSERT INTO enrichment_jobs VALUES('metadata','admin','completed','{}','2026-10-23T00:00:00Z','2026-10-23T00:00:00Z')`,
	} {
		if _, err = db.ExecContext(t.Context(), statement); err != nil {
			t.Fatal(err)
		}
	}
	for _, reason := range []string{"automatic", "importLyrics", "importArtwork"} {
		_, err = db.ExecContext(t.Context(), `INSERT INTO metadata_automation VALUES(?,'admin','scan',1,'digest','music','track','asset','v1',?,'completed',?,'metadata','SOURCE_OFFLINE','2026-10-23T00:00:00Z')`, reason, reason, []byte(`{"enabled":true}`))
		if err != nil {
			t.Fatal(err)
		}
		_, err = db.ExecContext(t.Context(), `INSERT INTO metadata_automation_actions VALUES(?,'artwork','',7,?)`, reason, []byte(`{"field":"artwork","status":"applied"}`))
		if err != nil {
			t.Fatal(err)
		}
	}
	want := metadataAutomationSnapshot(t, db)
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := sqlite.Open(t.Context(), filename)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	if _, err = os.Stat(filename + ".before-032"); err != nil {
		t.Fatal("migration backup is missing", err)
	}
	if got := metadataAutomationSnapshot(t, store.DB); !reflect.DeepEqual(got, want) {
		t.Fatalf("migration changed persisted automation: got %v, want %v", got, want)
	}
	_, err = store.DB.ExecContext(t.Context(), `INSERT INTO metadata_automation SELECT 'refresh',owner,scan_job_id,scan_generation,plan_digest,root_id,source_id,asset_id,asset_version,'refreshMetadata','queued',NULL,NULL,NULL,created_at FROM metadata_automation WHERE id='automatic'`)
	if err != nil {
		t.Fatal("metadata refresh reason was not accepted", err)
	}
	for _, statement := range []string{
		`UPDATE metadata_automation SET reason='invalid' WHERE id='refresh'`,
		`UPDATE metadata_automation SET state='invalid' WHERE id='refresh'`,
		`UPDATE metadata_automation SET source_id='missing' WHERE id='refresh'`,
		`UPDATE metadata_automation SET scan_job_id='missing' WHERE id='refresh'`,
		`UPDATE metadata_automation SET job_id='missing' WHERE id='refresh'`,
		`INSERT INTO metadata_automation SELECT 'duplicate',owner,scan_job_id,scan_generation,plan_digest,root_id,source_id,asset_id,asset_version,reason,state,policy,job_id,failure_code,created_at FROM metadata_automation WHERE id='refresh'`,
		`INSERT INTO metadata_automation_actions VALUES('missing','artwork','',0,'{}')`,
		`DELETE FROM metadata_automation WHERE id='automatic'`,
	} {
		if _, err = store.DB.ExecContext(t.Context(), statement); err == nil {
			t.Fatal("migration lost a constraint", statement)
		}
	}
	for _, index := range []string{"metadata_automation_queue", "metadata_automation_scan"} {
		var count int
		err = store.DB.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM sqlite_schema WHERE type='index' AND tbl_name='metadata_automation' AND name=?", index).Scan(&count)
		if err != nil || count != 1 {
			t.Fatal("migration lost automation index", index, count, err)
		}
	}
	rows, err := store.DB.QueryContext(t.Context(), "PRAGMA foreign_key_check")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() || rows.Err() != nil {
		t.Fatal("migration left invalid foreign keys", rows.Err())
	}
}

func metadataAutomationSnapshot(t *testing.T, db *sql.DB) [][]any {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), `SELECT a.*,b.* FROM metadata_automation a JOIN metadata_automation_actions b ON b.trigger_id=a.id ORDER BY a.id,b.field,b.language`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	var result [][]any
	for rows.Next() {
		values, pointers := make([]any, len(columns)), make([]any, len(columns))
		for i := range values {
			pointers[i] = &values[i]
		}
		if err = rows.Scan(pointers...); err != nil {
			t.Fatal(err)
		}
		for i, value := range values {
			if raw, ok := value.([]byte); ok {
				values[i] = string(raw)
			}
		}
		result = append(result, values)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	return result
}
