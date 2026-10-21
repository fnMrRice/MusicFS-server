package plugins_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/library/jobs"
)

type coordinatedScanQueue struct {
	jobs.Repository
	mu       sync.Mutex
	next     *jobs.Job
	finished chan error
}

func (q *coordinatedScanQueue) RecoverJobs(context.Context) error { return nil }
func (q *coordinatedScanQueue) Enqueue(_ context.Context, owner, provider string, plan pluginproto.ScanPlan, _ string) (jobs.Job, bool, error) {
	digest, err := plan.Digest()
	if err != nil {
		return jobs.Job{}, false, err
	}
	j := jobs.Job{ID: domain.NewID(), Owner: owner, ProviderInstanceID: provider, Kind: "scan-full", Generation: 1, Plan: plan, PlanDigest: digest}
	q.mu.Lock()
	q.next = &j
	q.mu.Unlock()
	return j, false, nil
}
func (q *coordinatedScanQueue) Claim(context.Context) (jobs.Job, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.next == nil {
		return jobs.Job{}, errors.New("no pending scan")
	}
	j := *q.next
	q.next = nil
	return j, nil
}
func (q *coordinatedScanQueue) Finish(_ context.Context, _ jobs.Job, _ int, err error) error {
	q.finished <- err
	return nil
}

type coordinatedPublication struct {
	library.Repository
	entered, release chan struct{}
}

func (r *coordinatedPublication) Publish(ctx context.Context, _ jobs.Job, _ library.ScanPublication) error {
	close(r.entered)
	select {
	case <-r.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestScanRetainsProviderReadThroughPublication(t *testing.T) {
	w, _, rebind := rebindFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := w.RebindLocalTrack(ctx, rebind); err != nil {
		t.Fatal(err)
	}
	repo := &coordinatedPublication{entered: make(chan struct{}), release: make(chan struct{})}
	queueRepo := &coordinatedScanQueue{finished: make(chan error, 1)}
	queue := jobs.New(queueRepo)
	configuration := w.Manifest.Configuration
	lib := library.New(repo, queue, []library.Runtime{{ID: "local", Owner: "owner", Worker: w, ConfigRevision: "test-config", Configuration: configuration, ImportDefaults: configuration.ImportDefaults}}, nil)
	if err := queue.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer queue.Close()
	_, _, err := lib.Scan(ctx, "owner", "local", library.ScanInput{Mode: "full", SchemaRevision: configuration.SchemaRevision, RootIDs: []string{"music"}}, "scan-coordination")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-repo.entered:
	case err = <-queueRepo.finished:
		t.Fatal("scan failed before publication", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if lease, err := lib.TryExclusive(ctx, "owner", "local", domain.NewID()); err == nil {
		lease.Close()
		t.Fatal("writeback entered while scan publication was pending")
	}
	close(repo.release)
	select {
	case err = <-queueRepo.finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	lease, err := lib.TryExclusive(ctx, "owner", "local", domain.NewID())
	if err != nil {
		t.Fatal("completed scan retained provider read", err)
	}
	lease.Close()
}
