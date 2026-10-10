package storage_test

import (
	"context"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
	"path/filepath"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/storage/sqlite"
)

func TestJobPreservesFailureCodeAndLastSuccessfulScan(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "jobs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, _, err := store.Enqueue(ctx, "admin", "root", testkit.ScanPlan("root"), "successful-job"); err != nil {
		t.Fatal(err)
	}
	first, err := store.Claim(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Finish(ctx, first, 2, nil); err != nil {
		t.Fatal(err)
	}
	prior, err := store.GetJob(ctx, "admin", first.ID)
	if err != nil || prior.LastSuccessAt == nil {
		t.Fatal("successful job has no completion time", err)
	}
	if _, _, err := store.Enqueue(ctx, "admin", "root", testkit.ScanPlan("root"), "offline-next-job"); err != nil {
		t.Fatal(err)
	}
	next, err := store.Claim(ctx)
	if err != nil || next.LastSuccessAt == nil || !next.LastSuccessAt.Equal(*prior.LastSuccessAt) {
		t.Fatal("running scan lost last successful scan timestamp", err)
	}
	if err := store.Finish(ctx, next, 0, domain.Fail("SOURCE_OFFLINE", "Provider is offline")); err != nil {
		t.Fatal(err)
	}
	failed, err := store.GetJob(ctx, "admin", next.ID)
	if err != nil || failed.Error == nil || failed.Error.Code != "SOURCE_OFFLINE" || failed.LastSuccessAt == nil || !failed.LastSuccessAt.Equal(*prior.LastSuccessAt) {
		t.Fatal("failed scan lost typed error or previous success time", err)
	}
}
