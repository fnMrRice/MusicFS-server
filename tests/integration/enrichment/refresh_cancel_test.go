package enrichment_test

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/src/metadata/enrichmentworker"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

type blockedRefreshApply struct {
	*sqlite.Store
	source                      string
	once                        sync.Once
	started, cancelled, release chan struct{}
}

func (r *blockedRefreshApply) ApplyAutomaticEnrichment(ctx context.Context, owner, candidate, object string, revision int64, key string, refresh bool, guard mediastore.Guard) (mediastore.Selection, error) {
	if refresh {
		value, err := r.Store.EnrichmentCandidate(ctx, owner, candidate)
		if err != nil {
			return mediastore.Selection{}, err
		}
		if value.Target.SourceTrackID == r.source {
			r.once.Do(func() {
				close(r.started)
				<-ctx.Done()
				close(r.cancelled)
				<-r.release
			})
		}
	}
	return r.Store.ApplyAutomaticEnrichment(ctx, owner, candidate, object, revision, key, refresh, guard)
}

func TestRefreshCancellationWaitsForAutomaticApplyResourceRelease(t *testing.T) {
	f := setup(t)
	f.service.Close()
	if err := os.WriteFile(filepath.Join(f.root, "good.png"), testkit.ScraperPNG(), 0600); err != nil {
		t.Fatal(err)
	}
	repo := &blockedRefreshApply{Store: f.store, source: f.good().ID, started: make(chan struct{}), cancelled: make(chan struct{}), release: make(chan struct{})}
	var release sync.Once
	defer release.Do(func() { close(repo.release) })
	service, err := enrichmentworker.New(repo, f.lib, f.content, f.runtimes, enrichmentworker.Config{})
	if err != nil {
		t.Fatal(err)
	}
	f.service = service
	t.Cleanup(service.Close)
	plan := testkit.ScanPlan("root")
	plan.RefreshMetadata = true
	scan := rescanAutomaticPlan(t, f, plan)
	if err = service.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-repo.started:
	case <-time.After(20 * time.Second):
		t.Fatal("automatic refresh did not reach publication")
	}
	state, err := f.store.CancelRefreshTask(t.Context(), "admin", scan.ID)
	if err != nil || state.Status != "cancelling" {
		t.Fatal(state, err)
	}
	ids, err := f.store.RefreshTaskMetadataJobs(t.Context(), "admin", scan.ID)
	if err != nil {
		t.Fatal(err)
	}
	done := service.InterruptJobs(ids)
	select {
	case <-repo.cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("automatic apply context was not cancelled")
	}
	select {
	case <-done:
		t.Fatal("cancellation finished before resource cleanup")
	default:
	}
	release.Do(func() { close(repo.release) })
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("automatic apply resources did not finish")
	}
	if err = f.store.CompleteRefreshTaskCancellation(t.Context(), "admin", scan.ID); err != nil {
		t.Fatal(err)
	}
	state, err = f.store.GetRefreshTask(t.Context(), "admin", scan.ID)
	if err != nil || state.Status != "cancelled" {
		t.Fatal(state, err)
	}
	selected, err := f.content.ContentSelection(t.Context(), "admin", f.good().ID)
	if err != nil || len(selected.Bindings) != 0 {
		t.Fatal("late automatic apply changed selected metadata", selected, err)
	}
}

type committedRefreshApply struct {
	*sqlite.Store
	once               sync.Once
	committed          chan mediastore.Selection
	cancelled, release chan struct{}
}

func (r *committedRefreshApply) ApplyAutomaticEnrichment(ctx context.Context, owner, candidate, object string, revision int64, key string, refresh bool, guard mediastore.Guard) (mediastore.Selection, error) {
	selected, err := r.Store.ApplyAutomaticEnrichment(ctx, owner, candidate, object, revision, key, refresh, guard)
	if err == nil && refresh {
		r.once.Do(func() {
			r.committed <- selected
			<-ctx.Done()
			close(r.cancelled)
			<-r.release
		})
	}
	return selected, err
}

func TestRefreshCancellationRetainsSuccessfulApplyResultBeforeStopping(t *testing.T) {
	f := setup(t)
	f.service.Close()
	repo := &committedRefreshApply{Store: f.store, committed: make(chan mediastore.Selection, 1), cancelled: make(chan struct{}), release: make(chan struct{})}
	var release sync.Once
	defer release.Do(func() { close(repo.release) })
	service, err := enrichmentworker.New(repo, f.lib, f.content, f.runtimes, enrichmentworker.Config{})
	if err != nil {
		t.Fatal(err)
	}
	f.service = service
	t.Cleanup(service.Close)
	plan := testkit.ScanPlan("root")
	plan.RefreshMetadata = true
	scan := rescanAutomaticPlan(t, f, plan)
	if err = service.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	var committed mediastore.Selection
	select {
	case committed = <-repo.committed:
	case <-time.After(20 * time.Second):
		t.Fatal("automatic refresh did not commit")
	}
	if committed.Revision != 1 || len(committed.Bindings) != 1 {
		t.Fatal("unexpected initial commit", committed)
	}
	state, err := f.store.CancelRefreshTask(t.Context(), "admin", scan.ID)
	if err != nil || state.Status != "cancelling" || state.Result.Updated != 0 {
		t.Fatal("apply result was reported before worker completion", state, err)
	}
	ids, err := f.store.RefreshTaskMetadataJobs(t.Context(), "admin", scan.ID)
	if err != nil {
		t.Fatal(err)
	}
	done := service.InterruptJobs(ids)
	select {
	case <-repo.cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("committed apply did not receive cancellation")
	}
	select {
	case <-done:
		t.Fatal("cancellation completed before apply result cleanup")
	default:
	}
	release.Do(func() { close(repo.release) })
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("committed apply did not finish cleanup")
	}
	if err = f.store.CompleteRefreshTaskCancellation(t.Context(), "admin", scan.ID); err != nil {
		t.Fatal(err)
	}
	state, err = f.store.GetRefreshTask(t.Context(), "admin", scan.ID)
	if err != nil || state.Status != "cancelled" || state.Result.Updated != 1 {
		t.Fatal("cancellation lost a successfully committed result", state, err)
	}
	current, err := f.content.ContentSelection(t.Context(), "admin", committed.Target.SourceTrackID)
	if err != nil || current.Revision != committed.Revision || len(current.Bindings) != 1 || current.Bindings[0].ID != committed.Bindings[0].ID {
		t.Fatal("cancellation replaced or discarded committed binding", current, err)
	}
}
