package storage_test

import (
	"encoding/json"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/metadata/matching"
	"github.com/fnMrRice/MusicFS-server/src/storage"
	"github.com/fnMrRice/MusicFS-server/src/tasks"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func enqueueLibraryOperation(t *testing.T, s tasks.OperationRepository, owner, kind, key, payload string) tasks.Task {
	t.Helper()
	op, err := s.EnqueueOperation(t.Context(), owner, kind, key, json.RawMessage(payload))
	if err != nil {
		t.Fatal(err)
	}
	return op
}

func TestLibraryOperationMixedPaginationOwnershipAndCancellation(t *testing.T) {
	checkMixedLibraryOperationQueue(t, evidenceStore(t))
}

func checkMixedLibraryOperationQueue(t *testing.T, store storage.Backend) {
	t.Helper()
	ctx := t.Context()
	plan := testkit.ScanPlan("music")
	plan.RefreshMetadata = true
	scan, _, err := store.Enqueue(ctx, "admin", "local", plan, "library-refresh")
	if err != nil {
		t.Fatal(err)
	}
	album := enqueueLibraryOperation(t, store, "admin", tasks.AlbumAutoScrape, "library-test", "{}")
	merge := enqueueLibraryOperation(t, store, "admin", tasks.BulkMerge, "library-test", `{"mode":"title_artist","expectedRevision":0}`)
	foreign := enqueueLibraryOperation(t, store, "other", tasks.AlbumAutoScrape, "library-test", "{}")
	replay := enqueueLibraryOperation(t, store, "admin", tasks.AlbumAutoScrape, "library-test", "{}")
	if replay.ID != album.ID {
		t.Fatal("duplicate request created another queued task")
	}
	_, err = store.EnqueueOperation(ctx, "admin", tasks.AlbumAutoScrape, "library-test", json.RawMessage(`{"q":"different"}`))
	matchingError(t, err, "CONFLICT")
	_, err = store.CancelRefreshTask(ctx, "admin", foreign.ID)
	matchingError(t, err, "NOT_FOUND")
	_, err = store.ListRefreshTasks(ctx, "admin", foreign.ID, 10)
	matchingError(t, err, "INVALID_ARGUMENT")
	admin, err := store.ListRefreshTasks(ctx, "", "", 10)
	if err != nil || admin.ActiveCount != 4 {
		t.Fatal(admin, err)
	}
	cursor := ""
	for _, want := range []string{merge.ID, album.ID, scan.ID} {
		page, err := store.ListRefreshTasks(ctx, "admin", cursor, 1)
		if err != nil || len(page.Items) != 1 || page.Items[0].ID != want || page.ActiveCount != 3 {
			t.Fatal("mixed task pagination or count is wrong", page, err)
		}
		cursor = want
	}
	for range 2 {
		task, err := store.CancelRefreshTask(ctx, "admin", merge.ID)
		if err != nil || task.Status != "cancelled" || !task.CancelRequested || task.Cancellable {
			t.Fatal("queue cancellation was not durable", task, err)
		}
	}
	for _, want := range []string{album.ID, foreign.ID} {
		op, err := store.ClaimOperation(ctx)
		if err != nil || op == nil || op.Task.ID != want || op.Task.Phase != "scraping" || op.Task.Cancellable {
			t.Fatal("wrong queued operation claimed", op, err)
		}
		unchanged, err := store.CancelRefreshTask(ctx, "", want)
		if err != nil || unchanged.Status != "running" || unchanged.CancelRequested {
			t.Fatal("running operation was cancelled", unchanged, err)
		}
	}
	if op, err := store.ClaimOperation(ctx); err != nil || op != nil {
		t.Fatal("cancelled or running task claimed", op, err)
	}
}

func TestLibraryOperationRecoveryRetainsProgressAndCommittedMerge(t *testing.T) {
	store, _, _ := bulkMergeFixture(t, 2, nil)
	ctx := t.Context()
	req := matching.BulkMergeRequest{Mode: "title_artist"}
	payload, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	merge := enqueueLibraryOperation(t, store, "admin", tasks.BulkMerge, "merge-committed", string(payload))
	album := enqueueLibraryOperation(t, store, "admin", tasks.AlbumAutoScrape, "scrape-interrupted", "{}")
	queued := enqueueLibraryOperation(t, store, "admin", tasks.BulkMerge, "waiting-merge", string(payload))
	for _, want := range []string{merge.ID, album.ID} {
		op, err := store.ClaimOperation(ctx)
		if err != nil || op == nil || op.Task.ID != want {
			t.Fatal(op, err)
		}
	}
	result, err := store.BulkMergeSummary(ctx, "admin", req, merge.ID)
	if err != nil || result.MergedGroups != 1 {
		t.Fatal(result, err)
	}
	p, r := tasks.Progress{Completed: 1, Total: 2}, tasks.Result{Updated: 1}
	if err := store.UpdateOperation(ctx, album.ID, p, r); err != nil {
		t.Fatal(err)
	}
	if err := store.RecoverOperations(ctx); err != nil {
		t.Fatal(err)
	}
	done, err := store.GetRefreshTask(ctx, "admin", merge.ID)
	if err != nil || done.Status != "succeeded" || done.Result.MergedGroups != 1 || done.Result.MergedSources != 2 || done.Error != nil {
		t.Fatal("lost committed merge receipt", done, err)
	}
	stopped, err := store.GetRefreshTask(ctx, "admin", album.ID)
	if err != nil || stopped.Status != "failed" || stopped.Error == nil || stopped.Progress != p || stopped.Result != r {
		t.Fatal("lost partial album results", stopped, err)
	}
	op, err := store.ClaimOperation(ctx)
	if err != nil || op == nil || op.Task.ID != queued.ID || op.Task.Phase != "merging" {
		t.Fatal("queued work did not survive restart", op, err)
	}
	_, err = store.BulkMerge(ctx, "admin", req, queued.ID)
	matchingError(t, err, "CONFLICT")
	failure := &domain.Error{Code: "CONFLICT", Message: "Matching revision changed"}
	for range 2 {
		if err := store.FinishOperation(ctx, queued.ID, "failed", tasks.Progress{}, tasks.Result{}, failure); err != nil {
			t.Fatal("could not record or replay completion", err)
		}
	}
	page, err := store.ListRefreshTasks(ctx, "admin", "", 10)
	if err != nil || page.ActiveCount != 0 {
		t.Fatal(page, err)
	}
}
