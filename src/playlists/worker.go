package playlists

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/domain"
)

func (s *Service) worker(ctx context.Context) {
	defer s.wg.Done()
	for ctx.Err() == nil {
		job, err := s.repo.ClaimRemoteSync(ctx)
		if err == nil {
			s.execute(ctx, job)
			continue
		}
		delay := 100 * time.Millisecond
		if !errors.Is(err, sql.ErrNoRows) {
			delay = time.Second
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-s.wake:
			timer.Stop()
		case <-timer.C:
		}
	}
}
func (s *Service) execute(parent context.Context, job SyncJob) {
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	s.mu.Lock()
	s.running[job.ID] = cancel
	s.mu.Unlock()
	defer func() { cancel(); s.mu.Lock(); delete(s.running, job.ID); s.mu.Unlock() }()
	current, err := s.repo.RemoteSyncJob(ctx, job.Owner, job.ID)
	if err == nil && (current.State != "running" || current.Generation != job.Generation) {
		return
	}
	if err == nil {
		if remote, ok := s.remote.(interface {
			Acquire(context.Context, string, Binding) (context.Context, func(), error)
		}); ok {
			var release func()
			ctx, release, err = remote.Acquire(ctx, job.Owner, job.Binding)
			if err == nil {
				defer release()
			}
		}
	}
	var binding Binding
	if err == nil {
		binding, err = s.authorize(ctx, job.Owner, job.Binding.RemoteRef)
	}
	if err == nil && binding != job.Binding {
		err = domain.Fail("RESOURCE_CHANGED", "Remote instance configuration changed while queued")
	}
	var observation Observation
	if err == nil {
		observation, err = s.remote.ObservePlaylist(ctx, job.Owner, binding)
	}
	if err == nil {
		err = ctx.Err()
	}
	if err == nil {
		_, err = s.repo.FinishRemoteSync(ctx, job, observation)
	}
	if parent.Err() != nil {
		return
	}
	if err != nil {
		finish, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		failure := &domain.Error{Code: FailureCode(err), Message: "Remote playlist synchronization failed; previous snapshot retained"}
		if finishErr := s.repo.FailRemoteSync(finish, job, failure); finishErr != nil {
			current, readErr := s.repo.RemoteSyncJob(finish, job.Owner, job.ID)
			if readErr != nil || current.State == "running" {
				slog.Error("Remote playlist task requires recovery", "jobId", job.ID, "errorCode", FailureCode(finishErr))
			}
		}
	}
}
