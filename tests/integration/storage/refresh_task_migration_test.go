package storage_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite/migrations"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestRefreshCancellationMigrationPreservesJobsAndDurableIntent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "refresh.sqlite")
	db := openMigrationFixture(t, path)
	steps, err := migrations.Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	for _, step := range steps {
		if step.Version > 32 {
			break
		}
		if _, err = db.Exec(step.SQL); err != nil {
			t.Fatal(step.Version, err)
		}
		if _, err = db.Exec("INSERT INTO schema_migrations VALUES(?)", step.Version); err != nil {
			t.Fatal(err)
		}
	}
	plan := testkit.ScanPlan("music")
	plan.RefreshMetadata = true
	plan, err = plan.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := plan.Digest()
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO jobs(id,owner,target,kind,generation,state,created_at,updated_at,plan,plan_digest)
 VALUES('refresh','admin','local','scan-full',1,'running','2026-10-23T00:00:00Z','2026-10-23T00:00:00Z',?,?)`, raw, digest)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := sqlite.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(path + ".before-033"); err != nil {
		t.Fatal("cancellation migration backup missing", err)
	}
	state, err := store.GetRefreshTask(t.Context(), "admin", "refresh")
	if err != nil || state.Status != "running" || state.CancelRequested {
		t.Fatal("upgrade changed existing job", state, err)
	}
	state, err = store.CancelRefreshTask(t.Context(), "admin", "refresh")
	if err != nil || state.Status != "cancelling" {
		t.Fatal(state, err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = sqlite.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	state, err = store.GetRefreshTask(t.Context(), "admin", "refresh")
	if err != nil || state.Status != "cancelling" {
		t.Fatal("cancellation intent was not durable", state, err)
	}
	if err = store.RecoverJobs(t.Context()); err != nil {
		t.Fatal(err)
	}
	state, err = store.GetRefreshTask(t.Context(), "admin", "refresh")
	if err != nil || state.Status != "cancelled" {
		t.Fatal("recovery did not confirm old resources are gone", state, err)
	}
	if _, err = store.DB.Exec("INSERT INTO refresh_task_cancellations VALUES('missing','now',NULL)"); err == nil {
		t.Fatal("cancellation foreign key missing")
	}
}
