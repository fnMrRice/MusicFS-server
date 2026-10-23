package storage_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/jobs"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestScanProgressRequiresCurrentExecutionAndRetainsDiscoveredCount(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "scan.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	queued, _, err := store.Enqueue(ctx, "admin", "local", testkit.ScanPlan("music"), "progress")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateProgress(ctx, queued, 1); err == nil {
		t.Fatal("queued task accepted progress")
	}
	active, err := store.Claim(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, observed := range []int{200, 200, 201} {
		if err := store.UpdateProgress(ctx, active, observed); err != nil {
			t.Fatal(err)
		}
	}
	current, err := store.GetJob(ctx, "admin", queued.ID)
	if err != nil || current.Status != "running" || current.Complete || current.Progress.Observed != 201 || current.UpdatedAt.Before(active.UpdatedAt) {
		t.Fatalf("running progress unavailable: %+v %v", current, err)
	}
	for name, change := range map[string]func(*jobs.Job){
		"owner":      func(j *jobs.Job) { j.Owner = "other" },
		"provider":   func(j *jobs.Job) { j.ProviderInstanceID = "other" },
		"generation": func(j *jobs.Job) { j.Generation++ },
		"digest":     func(j *jobs.Job) { j.PlanDigest = "other" },
		"plan":       func(j *jobs.Job) { j.Plan.Mode = "incremental" },
	} {
		t.Run(name, func(t *testing.T) {
			stale := active
			change(&stale)
			if err := store.UpdateProgress(ctx, stale, 202); err == nil {
				t.Fatal("changed execution accepted progress")
			}
		})
	}
	for _, observed := range []int{-1, 200, 50001} {
		if err := store.UpdateProgress(ctx, active, observed); err == nil {
			t.Fatalf("invalid progress accepted: %d", observed)
		}
	}
	if _, err := store.DB.ExecContext(ctx, "UPDATE provider_scan_leases SET active_job_id=NULL WHERE active_job_id=?", active.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateProgress(ctx, active, 202); err == nil {
		t.Fatal("execution without a lease accepted progress")
	}
	if _, err := store.DB.ExecContext(ctx, "UPDATE provider_scan_leases SET active_job_id=? WHERE owner=? AND provider=?", active.ID, active.Owner, active.ProviderInstanceID); err != nil {
		t.Fatal(err)
	}
	if err := store.Finish(ctx, active, 1, jobs.Retryable(domain.Fail("TEMPORARY_FAILURE", "Retry scan"))); err != nil {
		t.Fatal(err)
	}
	waiting, err := store.GetJob(ctx, "admin", active.ID)
	if err != nil || waiting.Progress.Observed != 201 || waiting.Status != "retry_wait" {
		t.Fatalf("failure lost discovered progress: %+v %v", waiting, err)
	}
	if err := store.UpdateProgress(ctx, active, 202); err == nil {
		t.Fatal("finished execution accepted progress")
	}
	next := claimScanWhenDue(t, store)
	if next.Progress.Observed != 0 {
		t.Fatal("retry did not reset attempt progress")
	}
	if err := store.UpdateProgress(ctx, active, 202); err == nil {
		t.Fatal("previous generation overwrote retry progress")
	}
	if err := store.UpdateProgress(ctx, next, 2); err != nil {
		t.Fatal(err)
	}
}
