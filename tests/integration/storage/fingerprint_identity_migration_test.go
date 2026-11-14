package storage_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite/migrations"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

func TestFingerprintIdentityMigrationArchivesOldInputsAndAllowsRecompute(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "fingerprints.sqlite")
	db := openMigrationFixture(t, path)
	db.SetMaxOpenConns(1)
	steps, err := migrations.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	for _, step := range steps {
		if step.Version > 33 {
			break
		}
		if _, err = db.Exec(step.SQL); err != nil {
			t.Fatal(step.Version, err)
		}
		if _, err = db.Exec("INSERT INTO schema_migrations VALUES(?)", step.Version); err != nil {
			t.Fatal(err)
		}
	}
	seedMigrationUser(t, db)
	old := &sqlite.Store{DB: db}
	// 在版本 033 的库上生成原始观测；普通曲目列表使用最新结构，升级后再验证。
	publishEvidenceObservations(t, old, evidenceObservation("preserved-track", "v1"))
	var sourceID string
	if err := db.QueryRow("SELECT id FROM tracks WHERE external_id='preserved-track'").Scan(&sourceID); err != nil {
		t.Fatal(err)
	}
	track, err := old.FindTrack(ctx, "admin", sourceID)
	if err != nil {
		t.Fatal(err)
	}
	job := fpClaim(t, old, fpTarget(track))
	publication, raw := fpPublication(t, job)
	files := fpFiles(t, t.TempDir())
	fpAdvance(t, old, files, publication, raw, "finished")
	target := fpTarget(track)
	target.StartMs++
	queued, _, err := old.QueueFingerprint(ctx, "admin", target, domain.NewID(), false)
	if err != nil {
		t.Fatal(err)
	}
	// 将新建夹具还原为迁移前的工具字段，确认运行代码不会再解码这些旧任务。
	for _, table := range []string{"fingerprint_jobs", "fingerprint_indexes", "media_publications"} {
		if _, err = db.Exec("UPDATE " + table + " SET document=replace(CAST(document AS TEXT),'\"analyzer\":','\"fpcalc\":')"); err != nil {
			t.Fatal(err)
		}
	}
	if err = old.Close(); err != nil {
		t.Fatal(err)
	}
	current, err := sqlite.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer current.Close()
	if _, err = os.Stat(path + ".before-034"); err != nil {
		t.Fatal("migration backup missing", err)
	}
	for _, id := range []string{job.ID, queued.ID} {
		if _, err = current.GetFingerprintJob(ctx, "admin", id); err == nil {
			t.Fatal("retired task remained runnable or publicly readable")
		}
	}
	var count int
	if err = current.DB.QueryRow("SELECT COUNT(*) FROM retired_fingerprint_evidence WHERE instr(CAST(document AS TEXT),'\"fpcalc\":')>0").Scan(&count); err != nil || count != 3 {
		t.Fatal("old evidence audit was not preserved", count, err)
	}
	if err = current.DB.QueryRow("SELECT COUNT(*) FROM fingerprint_indexes").Scan(&count); err != nil || count != 0 {
		t.Fatal("obsolete evidence remained eligible", err)
	}
	if err = current.DB.QueryRow("SELECT COUNT(*) FROM idempotency WHERE substr(route,1,12)='fingerprint/'").Scan(&count); err != nil || count != 0 {
		t.Fatal("old task replay survived migration", err)
	}
	p, err := current.GetPublication(ctx, "admin", publication.ID)
	if err != nil || p.Status != "aborted" {
		t.Fatal("old publication was not retired consistently", err)
	}
	if err = current.DB.QueryRow("SELECT COUNT(*) FROM users WHERE id='existing-user'").Scan(&count); err != nil || count != 1 {
		t.Fatal("unrelated data changed", err)
	}
	tracks, err := current.ListTracks(ctx, "admin", "local", "", 10)
	if err != nil || len(tracks.Items) != 1 || tracks.Items[0].ID != track.ID {
		t.Fatal("track identity changed", err)
	}
	if _, _, err = current.QueueFingerprint(ctx, "admin", fpTarget(track), domain.NewID(), false); err != nil {
		t.Fatal("recomputation was blocked", err)
	}
}
