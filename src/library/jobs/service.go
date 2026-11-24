package jobs

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

type Service struct {
	// OnChanged 在启动调度前设置，任务状态提交后通知当前用户。
	OnChanged func(owner, jobID string, refreshMetadata bool)

	repo     Repository
	handlers map[string]Handler
	mu       sync.Mutex
	cancel   context.CancelFunc
	done     chan struct{}
	failure  error
	running  map[string]execution
}

func New(repo Repository) *Service {
	return &Service{repo: repo, handlers: map[string]Handler{}, running: map[string]execution{}}
}
func (s *Service) Register(kind string, h Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers[kind] = h
}
func (s *Service) Err() error { s.mu.Lock(); defer s.mu.Unlock(); return s.failure }
func (s *Service) Submit(ctx context.Context, user, target string, plan pluginproto.ScanPlan, key string) (Job, bool, error) {
	if err := s.Err(); err != nil {
		return Job{}, false, err
	}
	job, merged, err := s.repo.Enqueue(ctx, user, target, plan, key)
	if err == nil {
		slog.InfoContext(ctx, "Scan task queued", "jobId", job.ID, "instanceId", target, "merged", merged, "roots", len(plan.Roots))
		if !merged {
			s.changed(job)
		}
	}
	return job, merged, err
}
func (s *Service) Get(ctx context.Context, user, id string) (Job, error) {
	j, err := s.repo.GetJob(ctx, user, id)
	if err == nil && !IsTerminal(j.Status) {
		if stopped := s.Err(); stopped != nil {
			return Job{}, stopped
		}
	}
	return j, err
}
func (s *Service) UpdateProgress(ctx context.Context, job Job, observed int) error {
	err := s.repo.UpdateProgress(ctx, job, observed)
	if err == nil {
		s.changed(job)
	}
	return err
}
func (s *Service) changed(job Job) {
	if s.OnChanged != nil {
		s.OnChanged(job.Owner, job.ID, job.Plan.RefreshMetadata)
	}
}
func (s *Service) ScanStatus(ctx context.Context, user, provider string) (ScanStatus, error) {
	status, err := s.repo.ScanStatus(ctx, user, provider)
	if err == nil && status.Job != nil && !IsTerminal(status.Job.Status) {
		if stopped := s.Err(); stopped != nil {
			return ScanStatus{}, stopped
		}
	}
	return status, err
}
func (s *Service) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done != nil {
		return domain.Fail("CONFLICT", "Scan scheduler is already started")
	}
	if err := s.repo.RecoverJobs(ctx); err != nil {
		return err
	}
	ctx, s.cancel = context.WithCancel(ctx)
	s.done = make(chan struct{})
	slog.DebugContext(ctx, "Scan scheduler started")
	go s.run(ctx)
	return nil
}
func (s *Service) run(ctx context.Context) {
	defer close(s.done)
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		job, err := s.repo.Claim(ctx)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			if ctx.Err() == nil {
				s.stopFailed()
			}
			return
		}
		s.changed(job)
		s.mu.Lock()
		h := s.handlers[job.Kind]
		s.mu.Unlock()
		started := time.Now()
		slog.InfoContext(ctx, "Scan task started", "jobId", job.ID, "instanceId", job.ProviderInstanceID, "attempt", job.AttemptCount)
		var n int
		if h == nil {
			err = domain.Fail("UNSUPPORTED", "Unsupported scan task type")
		} else {
			n, err = s.execute(ctx, job, h)
		}
		if !s.finish(job, n, err) {
			s.stopFailed()
			return
		}
		s.changed(job)
		s.logFinished(job, time.Since(started))
	}
}

// Completion has a separate bounded persistence budget even after shutdown.
// Stop claiming if durable state cannot be recorded; a restart recovers the
// running attempt without granting an unlimited fresh execution budget.
func (s *Service) finish(job Job, n int, failure error) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for attempt := 0; attempt < 3; attempt++ {
		err := s.repo.Finish(ctx, job, n, failure)
		if err == nil {
			return true
		}
		// An ambiguous commit may already have persisted the transition.
		// A conflict alone is insufficient: verify that this execution really
		// ended or was superseded instead of silently stranding a running task.
		if current, e := s.repo.GetJob(ctx, job.Owner, job.ID); e == nil && (current.Generation != job.Generation || current.Status != "running") {
			return true
		}
		if attempt == 2 {
			break
		}
		slog.DebugContext(ctx, "Scan task state persistence retry", "jobId", job.ID, "attempt", attempt+1, "errorCode", "TEMPORARY_FAILURE")
		timer := time.NewTimer(time.Duration(attempt+1) * 100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return false
		case <-timer.C:
		}
	}
	return false
}
func (s *Service) stopFailed() {
	s.mu.Lock()
	s.failure = domain.Fail("TEMPORARY_FAILURE", "Scan scheduler stopped because durable task state is unavailable; restart after resolving storage failure")
	s.mu.Unlock()
	slog.Error("Scan scheduler stopped because durable task state is unavailable")
}
func (s *Service) Close() {
	s.mu.Lock()
	cancel, done := s.cancel, s.done
	s.mu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
}
