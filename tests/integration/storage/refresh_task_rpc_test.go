package storage_test

import (
	"context"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/storage"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestRefreshTaskPersistenceWorkerContract(t *testing.T) {
	options := storageWorker(t)
	options.DataRoot = t.TempDir()
	ctx := context.Background()
	store, err := storage.Open(ctx, "plugin", options)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	plan := testkit.ScanPlan("music")
	plan.RefreshMetadata = true
	job, _, err := store.Enqueue(ctx, "owner", "local", plan, "refresh-rpc-task")
	if err != nil {
		t.Fatal(err)
	}
	page, err := store.ListRefreshTasks(ctx, "owner", "", 200)
	if err != nil || len(page.Items) != 1 || page.ActiveCount != 1 || page.Items[0].ID != job.ID {
		t.Fatal("task page RPC failed", page, err)
	}
	active, err := store.Claim(ctx)
	if err != nil || active.ID != job.ID {
		t.Fatal(active, err)
	}
	task, err := store.CancelRefreshTask(ctx, "owner", job.ID)
	if err != nil || task.Status != "cancelling" {
		t.Fatal("cancel RPC failed", task, err)
	}
	ids, err := store.RefreshTaskMetadataJobs(ctx, "owner", job.ID)
	if err != nil || len(ids) != 0 {
		t.Fatal("metadata task IDs RPC failed", ids, err)
	}
	if err = store.CompleteRefreshTaskCancellation(ctx, "owner", job.ID); err != nil {
		t.Fatal(err)
	}
	task, err = store.GetRefreshTask(ctx, "owner", job.ID)
	if err != nil || task.Status != "cancelled" || !task.CancelRequested {
		t.Fatal("task state RPC failed", task, err)
	}
	if _, err = store.GetRefreshTask(ctx, "other", job.ID); err == nil {
		t.Fatal("RPC exposed another owner's task")
	}
}
