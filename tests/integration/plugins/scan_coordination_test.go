package plugins_test

import (
	"context"
	"database/sql"
	"sync"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/library/jobs"
)

type coordinatedScanQueue struct {
	mu       sync.Mutex
	job      jobs.Job
	finished chan error
}

func (q *coordinatedScanQueue) RecoverJobs(context.Context) error { return nil }
func (q *coordinatedScanQueue) Enqueue(_ context.Context, owner, provider string, plan pluginproto.ScanPlan, _ string) (jobs.Job, bool, error) {
	digest, err := plan.Digest()
	if err != nil {
		return jobs.Job{}, false, err
	}
	now := time.Now().UTC()
	j := jobs.Job{ID: domain.NewID(), Owner: owner, ProviderInstanceID: provider, Kind: "scan-full", Plan: plan, PlanDigest: digest, Status: "queued", MaxAttempts: jobs.MaxAttempts, CreatedAt: now, UpdatedAt: now}
	q.mu.Lock()
	q.job = j
	q.mu.Unlock()
	return j, false, nil
}
func (q *coordinatedScanQueue) Claim(context.Context) (jobs.Job, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	now := time.Now().UTC()
	if q.job.Status != "queued" && (q.job.Status != "retry_wait" || q.job.NextAttemptAt == nil || now.Before(*q.job.NextAttemptAt)) {
		return jobs.Job{}, sql.ErrNoRows
	}
	q.job.Status = "running"
	q.job.Generation++
	q.job.AttemptCount++
	q.job.RetryCount = q.job.AttemptCount - 1
	q.job.NextAttemptAt, q.job.LastAttemptAt, q.job.UpdatedAt = nil, &now, now
	q.job.Progress.Observed = 0
	return q.job, nil
}
func (q *coordinatedScanQueue) GetJob(_ context.Context, owner, id string) (jobs.Job, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.job.ID != id || q.job.Owner != owner {
		return jobs.Job{}, domain.Fail("NOT_FOUND", "Job not found")
	}
	return q.job, nil
}
func (q *coordinatedScanQueue) Finish(_ context.Context, given jobs.Job, observed int, failure error) error {
	q.mu.Lock()
	if q.job.ID != given.ID || q.job.Owner != given.Owner || q.job.Generation != given.Generation || q.job.Status != "running" {
		q.mu.Unlock()
		return domain.Fail("CONFLICT", "Scan task is no longer active")
	}
	now := time.Now().UTC()
	decision := jobs.DecideRetry(failure, q.job.AttemptCount, now)
	q.job.Status, q.job.Error, q.job.NextAttemptAt = decision.Status, decision.Error, decision.NextAttemptAt
	q.job.Progress.Observed = observed
	q.job.Complete, q.job.UpdatedAt = failure == nil, now
	if failure == nil {
		q.job.LastSuccessAt = &now
	}
	q.mu.Unlock()
	q.finished <- failure
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
	job, _, err := lib.Scan(ctx, "owner", "local", library.ScanInput{Mode: "full", SchemaRevision: configuration.SchemaRevision, RootIDs: []string{"music"}}, "scan-coordination")
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
	current, err := queue.Get(ctx, "owner", job.ID)
	if err != nil || current.Status != "succeeded" || !current.Complete || current.AttemptCount != 1 {
		t.Fatal("completed scan state was not retained", current.Status, current.Complete, current.AttemptCount, err)
	}
	lease, err := lib.TryExclusive(ctx, "owner", "local", domain.NewID())
	if err != nil {
		t.Fatal("completed scan retained provider read", err)
	}
	lease.Close()
}
