package jobs_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/library/jobs"
)

func TestScanInterruptionWaitsForHandlerResourceRelease(t *testing.T) {
	repo := &finishRepository{job: jobs.Job{ID: "refresh", Owner: "owner", Kind: "scan-full", Status: "queued", Generation: 1}}
	service := jobs.New(repo)
	started, cancelled, released := make(chan struct{}), make(chan struct{}), make(chan struct{})
	service.Register("scan-full", func(ctx context.Context, _ jobs.Job) (int, error) {
		close(started)
		<-ctx.Done()
		close(cancelled)
		<-released
		return 0, ctx.Err()
	})
	if err := service.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("scan did not start")
	}
	<-service.InterruptJob("another-owner", "refresh")
	select {
	case <-cancelled:
		t.Fatal("another owner interrupted the scan")
	default:
	}
	done := service.InterruptJob("owner", "refresh")
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("scan was not interrupted")
	}
	select {
	case <-done:
		t.Fatal("resources were reported released too early")
	default:
	}
	close(released)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("scan release was not reported")
	}
}

type cancelledBeforeStart struct{ finishRepository }

func (r *cancelledBeforeStart) Claim(ctx context.Context) (jobs.Job, error) {
	claimed, err := r.finishRepository.Claim(ctx)
	if err == nil {
		r.mu.Lock()
		r.job.Status = "cancelled"
		r.mu.Unlock()
	}
	return claimed, err
}

func TestScanCancelledBetweenClaimAndExecutionDoesNotOpenResources(t *testing.T) {
	repo := &cancelledBeforeStart{finishRepository{job: jobs.Job{ID: "refresh", Owner: "owner", Kind: "scan-full", Status: "queued", Generation: 1}}}
	service := jobs.New(repo)
	var calls atomic.Int32
	service.Register("scan-full", func(context.Context, jobs.Job) (int, error) { calls.Add(1); return 0, nil })
	if err := service.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		repo.mu.Lock()
		finished := repo.finishes > 0
		repo.mu.Unlock()
		if finished {
			if calls.Load() != 0 {
				t.Fatal("cancelled task opened resources")
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("cancelled scan did not finish")
}
