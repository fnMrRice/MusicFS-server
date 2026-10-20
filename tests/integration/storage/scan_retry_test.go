package storage_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/jobs"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func claimScanWhenDue(t *testing.T, s *sqlite.Store) jobs.Job {
	t.Helper()
	until := time.Now().Add(4 * time.Second)
	for time.Now().Before(until) {
		j, err := s.Claim(context.Background())
		if err == nil {
			return j
		}
		if !errors.Is(err, sql.ErrNoRows) {
			t.Fatal(err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("scan did not become due")
	return jobs.Job{}
}

func TestScanRetryPersistsDeadlinePlanAndFiniteBudget(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "retry.db")
	s, err := sqlite.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	plan := testkit.ScanPlan("music")
	queued, _, err := s.Enqueue(ctx, "admin", "local", plan, "first-request")
	if err != nil {
		t.Fatal(err)
	}
	if queued.AttemptCount != 0 || queued.RetryCount != 0 || queued.MaxAttempts != 3 || queued.LastAttemptAt != nil || queued.NextAttemptAt != nil || queued.CreatedAt.IsZero() {
		t.Fatalf("wrong initial budget: %+v", queued)
	}
	first := claimScanWhenDue(t, s)
	if first.AttemptCount != 1 || first.RetryCount != 0 || first.LastAttemptAt == nil {
		t.Fatal("first claim did not consume attempt")
	}
	transient := jobs.Retryable(domain.Fail("TEMPORARY_FAILURE", "Provider temporarily unavailable"))
	if err = s.Finish(ctx, first, 4, transient); err != nil {
		t.Fatal(err)
	}
	waiting, err := s.GetJob(ctx, "admin", first.ID)
	if err != nil || waiting.Status != "retry_wait" || waiting.Error == nil || waiting.NextAttemptAt == nil || waiting.Complete || waiting.Progress.Observed != 4 {
		t.Fatal(waiting, err)
	}
	if _, err = s.Claim(ctx); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("backoff was bypassed", err)
	}
	for _, key := range []string{"first-request", "manual-again"} {
		replay, merged, e := s.Enqueue(ctx, "admin", "local", plan, key)
		if e != nil || !merged || !reflect.DeepEqual(replay, waiting) {
			t.Fatal("repeated request changed budget/deadline", e)
		}
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = sqlite.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.RecoverJobs(ctx); err != nil {
		t.Fatal(err)
	}
	restored, err := s.GetJob(ctx, "admin", first.ID)
	if err != nil || !reflect.DeepEqual(restored, waiting) {
		t.Fatal("restart reinterpreted waiting task", err)
	}
	for attempt := 2; attempt <= 3; attempt++ {
		current := claimScanWhenDue(t, s)
		if current.ID != first.ID || current.Generation <= first.Generation || current.AttemptCount != attempt || current.RetryCount != attempt-1 || current.NextAttemptAt != nil || current.Error == nil || !reflect.DeepEqual(current.Plan, first.Plan) {
			t.Fatalf("bad retry: %+v", current)
		}
		if err = s.Publish(ctx, first, testkit.ScanPublication(first, nil)); err == nil {
			t.Fatal("old generation published into retry")
		}
		if err = s.Finish(ctx, first, 100, nil); err == nil {
			t.Fatal("old generation finished retry")
		}
		if err = s.Finish(ctx, current, attempt, transient); err != nil {
			t.Fatal(err)
		}
	}
	final, err := s.GetJob(ctx, "admin", first.ID)
	if err != nil || final.Status != "failed" || final.AttemptCount != 3 || final.RetryCount != 2 || final.NextAttemptAt != nil || final.Error == nil {
		t.Fatal(final, err)
	}
	if err = s.RecoverJobs(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Claim(ctx); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("exhausted task restarted", err)
	}
}

func TestScanCrashRecoveryConsumesBudgetAndDoesNotRedrawWait(t *testing.T) {
	s := evidenceStore(t)
	ctx := context.Background()
	j, _, err := s.Enqueue(ctx, "admin", "local", testkit.ScanPlan("music"), "crash-retries")
	if err != nil {
		t.Fatal(err)
	}
	for attempt := 1; attempt <= 3; attempt++ {
		active := claimScanWhenDue(t, s)
		if active.AttemptCount != attempt {
			t.Fatal("crash refreshed execution budget")
		}
		if err = s.RecoverJobs(ctx); err != nil {
			t.Fatal(err)
		}
		interrupted, err := s.GetJob(ctx, "admin", j.ID)
		if err != nil {
			t.Fatal(err)
		}
		if interrupted.Generation != 0 || interrupted.AttemptCount != attempt || interrupted.Error == nil {
			t.Fatal("recovery lost attempt audit")
		}
		if attempt < 3 && interrupted.Status != "retry_wait" || attempt == 3 && interrupted.Status != "failed" {
			t.Fatal("recovery ignored finite budget", interrupted.Status)
		}
		if err = s.RecoverJobs(ctx); err != nil {
			t.Fatal(err)
		}
		again, err := s.GetJob(ctx, "admin", j.ID)
		if err != nil || !reflect.DeepEqual(again, interrupted) {
			t.Fatal("second restart refreshed timer", err)
		}
		if err = s.Finish(ctx, active, 1, nil); err == nil {
			t.Fatal("interrupted worker published")
		}
	}
}

func TestScanPausedAndPermanentFailuresDoNotBusyRetry(t *testing.T) {
	for _, test := range []struct{ code, status string }{{"AUTH_REQUIRED", "paused"}, {"ACCESS_DENIED", "paused"}, {"SOURCE_OFFLINE", "paused"}, {"RESOURCE_CHANGED", "failed"}, {"UNSUPPORTED", "failed"}} {
		t.Run(test.code, func(t *testing.T) {
			s := evidenceStore(t)
			ctx := context.Background()
			plan := testkit.ScanPlan("music")
			j, _, err := s.Enqueue(ctx, "admin", "local", plan, "pause-key-1")
			if err != nil {
				t.Fatal(err)
			}
			active := claimScanWhenDue(t, s)
			if err = s.Finish(ctx, active, 1, jobs.Retryable(domain.Fail(test.code, "Provider requires attention"))); err != nil {
				t.Fatal(err)
			}
			stopped, err := s.GetJob(ctx, "admin", j.ID)
			if err != nil || stopped.Status != test.status || stopped.NextAttemptAt != nil {
				t.Fatal(stopped, err)
			}
			if _, err = s.Claim(ctx); !errors.Is(err, sql.ErrNoRows) {
				t.Fatal("terminal task automatically resumed", err)
			}
			replay, merged, err := s.Enqueue(ctx, "admin", "local", plan, "pause-key-1")
			if err != nil || !merged || !reflect.DeepEqual(replay, stopped) {
				t.Fatal("same operation revived task", err)
			}
			manual, merged, err := s.Enqueue(ctx, "admin", "local", plan, "new-manual-operation")
			if err != nil || merged || manual.ID == j.ID || manual.AttemptCount != 0 {
				t.Fatal("explicit new operation could not queue", err)
			}
		})
	}
}

func TestScanSchedulerRunsOtherJobsDuringBackoff(t *testing.T) {
	s := evidenceStore(t)
	ctx := context.Background()
	service := jobs.New(s)
	first, _, err := service.Submit(ctx, "admin", "first", testkit.ScanPlan("music"), "scheduled-first")
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := service.Submit(ctx, "admin", "second", testkit.ScanPlan("music"), "scheduled-second")
	if err != nil {
		t.Fatal(err)
	}
	order := make(chan string, 3)
	service.Register("scan-full", func(ctx context.Context, j jobs.Job) (int, error) {
		order <- j.ProviderInstanceID
		if j.ID == first.ID && j.AttemptCount == 1 {
			return 0, jobs.Retryable(domain.Fail("TEMPORARY_FAILURE", "Temporary failure"))
		}
		return 7, nil
	})
	if err = service.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	for _, want := range []string{"first", "second", "first"} {
		select {
		case actual := <-order:
			if actual != want {
				t.Fatal("backoff blocked other tasks", actual, want)
			}
		case <-time.After(4 * time.Second):
			t.Fatal("scheduler did not retry")
		}
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		j, e := s.GetJob(ctx, "admin", first.ID)
		if e == nil && j.Status == "succeeded" {
			if j.AttemptCount != 2 || j.RetryCount != 1 || j.Error != nil || !j.Complete || j.LastSuccessAt == nil {
				t.Fatal("success retained failure", j)
			}
			other, e := s.GetJob(ctx, "admin", second.ID)
			if e != nil || other.Status != "succeeded" {
				t.Fatal(other, e)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("retry completion was not persisted")
}

func TestScanSchedulerShutdownPersistsCancellation(t *testing.T) {
	s := evidenceStore(t)
	ctx := context.Background()
	service := jobs.New(s)
	j, _, err := service.Submit(ctx, "admin", "local", testkit.ScanPlan("music"), "shutdown-scan-task")
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	service.Register("scan-full", func(ctx context.Context, j jobs.Job) (int, error) { close(started); <-ctx.Done(); return 2, ctx.Err() })
	if err = service.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("scan did not start")
	}
	done := make(chan struct{})
	go func() { service.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled scan did not finish durable shutdown")
	}
	stopped, err := s.GetJob(ctx, "admin", j.ID)
	if err != nil || stopped.Status != "cancelled" || stopped.AttemptCount != 1 || stopped.NextAttemptAt != nil || stopped.Error == nil || stopped.Error.Code != "CANCELED" || stopped.Progress.Observed != 2 {
		t.Fatal("shutdown lost cancellation state", stopped, err)
	}
	if err = s.RecoverJobs(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Claim(ctx); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("cancelled task resumed after restart", err)
	}
}
