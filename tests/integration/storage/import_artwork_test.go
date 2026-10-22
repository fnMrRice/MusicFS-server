package storage_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite/migrations"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestImportArtworkOutboxHonorsRootOptions(t *testing.T) {
	s := evidenceStore(t)
	plan := testkit.ScanPlan("enabled", "disabled")
	plan.Options.ImportArtwork = true
	plan.RootOptions = map[string]pluginproto.ImportOptions{"disabled": {Recursive: true}}
	job := publishScope(t, s, plan, scopeObservation("with-artwork", "enabled", "song.mp3"), scopeObservation("without-artwork", "disabled", "song.mp3"))
	rows, err := s.DB.QueryContext(t.Context(), "SELECT root_id FROM metadata_automation WHERE scan_job_id=? AND reason='importArtwork'", job.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var roots []string
	for rows.Next() {
		var root string
		if err = rows.Scan(&root); err != nil {
			t.Fatal(err)
		}
		roots = append(roots, root)
	}
	if err = rows.Err(); err != nil || len(roots) != 1 || roots[0] != "enabled" {
		t.Fatal("artwork import leaked across root settings", roots, err)
	}
}

func TestImportArtworkMigrationPreservesTriggersActionsAndForeignKeys(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "before-artwork.sqlite")
	db := openMigrationFixture(t, filename)
	steps, err := migrations.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(t.Context(), "CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	for _, step := range steps {
		if step.Version > 28 {
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
		`INSERT INTO jobs(id,owner,target,kind,generation,state,created_at,updated_at,plan,plan_digest) VALUES('scan','admin','local','scan',1,'succeeded','2026-10-22T00:00:00Z','2026-10-22T00:00:00Z','{}','digest')`,
		`INSERT INTO tracks(id,owner,provider,external_id,root_id,path,version,asset_id,mime,size,document) VALUES('track','admin','local','song','music','song.mp3','v1','asset','audio/mpeg',100,'{}')`,
		`INSERT INTO metadata_automation(id,owner,scan_job_id,scan_generation,plan_digest,root_id,source_id,asset_id,asset_version,reason,state,policy,created_at) VALUES('trigger','admin','scan',1,'digest','music','track','asset','v1','importLyrics','completed','{"enabled":true}','2026-10-22T00:00:00Z')`,
		`INSERT INTO metadata_automation_actions VALUES('trigger','lyrics','en',7,'{"field":"lyrics","status":"applied"}')`,
	} {
		if _, err = db.ExecContext(t.Context(), statement); err != nil {
			t.Fatal(err)
		}
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := sqlite.Open(t.Context(), filename)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	if _, err = os.Stat(filename + ".before-029"); err != nil {
		t.Fatal("migration backup is missing", err)
	}
	var reason, state string
	var policy, action []byte
	var revision int
	err = store.DB.QueryRowContext(t.Context(), `SELECT a.reason,a.state,a.policy,b.expected_revision,b.document FROM metadata_automation a JOIN metadata_automation_actions b ON b.trigger_id=a.id WHERE a.id='trigger'`).Scan(&reason, &state, &policy, &revision, &action)
	if err != nil || reason != "importLyrics" || state != "completed" || revision != 7 || !bytes.Equal(policy, []byte(`{"enabled":true}`)) || !bytes.Equal(action, []byte(`{"field":"lyrics","status":"applied"}`)) {
		t.Fatal("migration changed persisted automation", reason, state, revision, err)
	}
	_, err = store.DB.ExecContext(t.Context(), `INSERT INTO metadata_automation SELECT 'artwork',owner,scan_job_id,scan_generation,plan_digest,root_id,source_id,asset_id,asset_version,'importArtwork','queued',NULL,NULL,NULL,created_at FROM metadata_automation WHERE id='trigger'`)
	if err != nil {
		t.Fatal("artwork reason was not accepted", err)
	}
	for _, statement := range []string{
		`UPDATE metadata_automation SET reason='invalid' WHERE id='artwork'`,
		`UPDATE metadata_automation SET source_id='missing' WHERE id='artwork'`,
		`INSERT INTO metadata_automation_actions VALUES('missing','artwork','',0,'{}')`,
		`DELETE FROM metadata_automation WHERE id='trigger'`,
	} {
		if _, err = store.DB.ExecContext(t.Context(), statement); err == nil {
			t.Fatal("migration lost a constraint", statement)
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
