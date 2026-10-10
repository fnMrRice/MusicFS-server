package jobs_test

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/jobs"
	"github.com/fnMrRice/MusicFS-server/src/pluginproto"
)

type finishRepository struct {
	jobs.Repository
	mu                         sync.Mutex
	job                        jobs.Job
	claims, finishes, failures int
	ambiguous                  bool
	conflict                   bool
}

func (r *finishRepository) RecoverJobs(context.Context) error { return nil }
func (r *finishRepository) Claim(context.Context) (jobs.Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.claims++
	if r.job.Status != "queued" {
		return jobs.Job{}, sql.ErrNoRows
	}
	r.job.Status = "running"
	return r.job, nil
}
func (r *finishRepository) GetJob(context.Context, string, string) (jobs.Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.job, nil
}
func (r *finishRepository) Finish(context.Context, jobs.Job, int, error) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.finishes++
	if r.ambiguous {
		r.job.Status = "succeeded"
		return errors.New("private ambiguous commit diagnostic")
	}
	if r.finishes <= r.failures {
		if r.conflict {
			return domain.Fail("CONFLICT", "Completion was rejected")
		}
		return errors.New("private storage failure")
	}
	r.job.Status = "succeeded"
	return nil
}
func TestCompletionPersistenceIsBoundedAndStopsNewClaims(t *testing.T) {
	r := &finishRepository{job: jobs.Job{ID: "task", Owner: "admin", Kind: "scan-full", Status: "queued", Generation: 1}, failures: 100}
	s := jobs.New(r)
	var handled atomic.Int32
	s.Register("scan-full", func(context.Context, jobs.Job) (int, error) { handled.Add(1); return 1, nil })
	if err := s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	deadline := time.Now().Add(2 * time.Second)
	for s.Err() == nil && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if s.Err() == nil || strings.Contains(s.Err().Error(), "private") {
		t.Fatal("durability failure not surfaced safely", s.Err())
	}
	r.mu.Lock()
	claims, finishes := r.claims, r.finishes
	r.mu.Unlock()
	if handled.Load() != 1 || finishes != 3 {
		t.Fatal("completion persistence did not use finite budget", handled.Load(), finishes)
	}
	time.Sleep(250 * time.Millisecond)
	r.mu.Lock()
	laterClaims := r.claims
	r.mu.Unlock()
	if laterClaims != claims {
		t.Fatal("failed scheduler continued claiming")
	}
	if _, err := s.Get(context.Background(), "admin", "task"); err == nil {
		t.Fatal("active job hid stopped scheduler")
	}
	if _, _, err := s.Submit(context.Background(), "admin", "local", pluginproto.ScanPlan{}, "new-request"); err == nil {
		t.Fatal("stopped scheduler accepted more jobs")
	}
}
func TestCompletionPersistenceRetriesAndRecognizesAmbiguousCommit(t *testing.T) {
	for _, ambiguous := range []bool{false, true} {
		r := &finishRepository{job: jobs.Job{ID: "task", Owner: "admin", Kind: "scan-full", Status: "queued", Generation: 1}, failures: 2, ambiguous: ambiguous}
		s := jobs.New(r)
		s.Register("scan-full", func(context.Context, jobs.Job) (int, error) { return 1, nil })
		if err := s.Start(context.Background()); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			j, _ := r.GetJob(context.Background(), "admin", "task")
			if j.Status == "succeeded" {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		s.Close()
		j, _ := r.GetJob(context.Background(), "admin", "task")
		want := 3
		if ambiguous {
			want = 1
		}
		if s.Err() != nil || j.Status != "succeeded" || r.finishes != want {
			t.Fatal("completion was lost", s.Err(), j.Status, r.finishes)
		}
	}
}

func TestCompletionConflictMustProveExecutionWasSuperseded(t *testing.T) {
	r := &finishRepository{job: jobs.Job{ID: "task", Owner: "admin", Kind: "scan-full", Status: "queued", Generation: 1}, failures: 100, conflict: true}
	s := jobs.New(r)
	s.Register("scan-full", func(context.Context, jobs.Job) (int, error) { return 1, nil })
	if err := s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	deadline := time.Now().Add(2 * time.Second)
	for s.Err() == nil && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if s.Err() == nil {
		t.Fatal("conflicting completion silently stranded an active execution")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.finishes != 3 || r.job.Status != "running" {
		t.Fatal("conflict bypassed bounded persistence")
	}
}
