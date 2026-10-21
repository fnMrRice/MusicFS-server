package storage_test

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/src/storage/migrations"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestScanPlanMigrationDiscardsOnlyUnplannedTaskCaches(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "unplanned.sqlite")
	uriPath := filepath.ToSlash(path)
	if !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath
	}
	u := url.URL{Scheme: "file", Path: uriPath}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.ExecContext(ctx, "CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	steps, err := migrations.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range steps {
		if step.Version > 6 {
			continue
		}
		if _, err = db.ExecContext(ctx, step.SQL); err != nil {
			t.Fatal(err)
		}
		if _, err = db.ExecContext(ctx, "INSERT INTO schema_migrations VALUES(?)", step.Version); err != nil {
			t.Fatal(err)
		}
	}
	for _, state := range []string{"queued", "running", "succeeded", "failed"} {
		if _, err = db.ExecContext(ctx, "INSERT INTO jobs(id,owner,target,kind,generation,state,created_at,updated_at) VALUES(?,'admin','local','scan-full',1,?,'2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')", state, state); err != nil {
			t.Fatal(err)
		}
	}
	for _, route := range []string{"scan/local", "scan/private", "playback/create", "hash/track"} {
		if _, err = db.ExecContext(ctx, "INSERT INTO idempotency VALUES('admin',?,'original-key','digest','result','2026-12-01T00:00:00Z')", route); err != nil {
			t.Fatal(err)
		}
	}
	statements := []string{
		"INSERT INTO tracks VALUES('track','admin','local','external','music','中文 空格.mp3','version','asset','audio/mpeg',10,'{\"track\":true}')",
		"INSERT INTO source_assets(id,owner,track_id,version,current,document,created_at) VALUES('asset','admin','track','version',1,'{\"asset\":true}','2026-01-01T00:00:00Z')",
		"INSERT INTO asset_evidence(id,owner,asset_id,kind,origin,generation,status,document,created_at) VALUES('proof','admin','asset','fileSha512','probe',1,'verified','{\"proof\":true}','2026-01-01T00:00:00Z')",
		"INSERT INTO hash_jobs VALUES('hash','admin','asset',1,'succeeded',0,'{\"hash\":true}','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')",
	}
	for _, statement := range statements {
		if _, err = db.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := sqlite.Open(ctx, path)
	if err != nil {
		t.Fatal("migration rejected existing scan caches", err)
	}
	defer s.Close()
	for query, want := range map[string]int{
		"SELECT COUNT(*) FROM jobs":                                        0,
		"SELECT COUNT(*) FROM idempotency WHERE substr(route,1,5)='scan/'": 0,
		"SELECT COUNT(*) FROM idempotency":                                 2,
		"SELECT COUNT(*) FROM tracks WHERE id='track' AND path='中文 空格.mp3' AND document='{\"track\":true}'":          1,
		"SELECT COUNT(*) FROM source_assets WHERE id='asset' AND current=1 AND document='{\"asset\":true}'":          1,
		"SELECT COUNT(*) FROM asset_evidence WHERE id='proof' AND status='verified' AND document='{\"proof\":true}'": 1,
		"SELECT COUNT(*) FROM hash_jobs WHERE id='hash' AND document='{\"hash\":true}'":                              1,
	} {
		var got int
		if err = s.DB.QueryRowContext(ctx, query).Scan(&got); err != nil || got != want {
			t.Fatalf("migration changed unexpected data: %s got=%d want=%d err=%v", query, got, want, err)
		}
	}
	if _, err = os.Stat(path + ".before-007"); err != nil {
		t.Fatal("migration backup missing", err)
	}
	if _, _, err = s.Enqueue(ctx, "admin", "local", testkit.ScanPlan("music"), "new-scan-plan"); err != nil {
		t.Fatal("new planned queue is unavailable", err)
	}
	if _, err = s.Claim(ctx); err != nil {
		t.Fatal("planned task could not claim after migration", err)
	}
}
