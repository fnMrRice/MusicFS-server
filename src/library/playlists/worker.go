package playlists

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

func (s *Service) worker(ctx context.Context, prepare bool) {
	defer s.wg.Done()
	var nextCheck time.Time
	for ctx.Err() == nil {
		job, err := s.repo.ClaimRemoteSync(ctx)
		if err == nil {
			s.jobChanged(job.Owner)
			s.execute(ctx, job)
			continue
		}
		delay := 100 * time.Millisecond
		if !errors.Is(err, sql.ErrNoRows) {
			slog.DebugContext(ctx, "Remote playlist claim deferred", "errorCode", FailureCode(err))
			delay = time.Second
		} else if prepare && s.preparePending != nil && !time.Now().Before(nextCheck) {
			s.preparePending(ctx)
			nextCheck = time.Now().Add(time.Minute)
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
	started := time.Now()
	// 单批读取仍有独立超时；完整歌单可能需要数百次受平台限速的批量请求。
	ctx, cancel := context.WithTimeout(parent, remoteSyncTimeout)
	s.mu.Lock()
	s.running[job.ID] = cancel
	s.mu.Unlock()
	defer func() { cancel(); s.mu.Lock(); delete(s.running, job.ID); s.mu.Unlock() }()
	current, err := s.repo.RemoteSyncJob(ctx, job.Owner, job.ID)
	if err == nil && (current.State != "running" || current.Generation != job.Generation) {
		return
	}
	slog.InfoContext(ctx, "Remote playlist task started", "jobId", job.ID)
	if err == nil {
		if remote, ok := s.remote.(interface {
			Acquire(context.Context, string, Binding) (context.Context, func(), error)
		}); ok {
			type lease struct {
				ctx     context.Context
				release func()
			}
			var acquired lease
			acquired, err = RetryRemoteRead(ctx, func(ctx context.Context) (lease, error) {
				leased, release, err := remote.Acquire(ctx, job.Owner, job.Binding)
				return lease{leased, release}, err
			})
			if err == nil {
				ctx = acquired.ctx
				defer acquired.release()
			}
		}
	}
	var observation Observation
	if err == nil {
		slog.DebugContext(ctx, "Remote playlist observation started", "jobId", job.ID)
		observation, err = RetryRemoteRead(ctx, func(ctx context.Context) (Observation, error) {
			binding, err := s.authorize(ctx, job.Owner, job.Binding.RemoteRef)
			if err != nil {
				return Observation{}, err
			}
			if binding != job.Binding {
				return Observation{}, domain.Fail("RESOURCE_CHANGED", "Remote instance configuration changed while queued")
			}
			return s.remote.ObservePlaylist(ctx, job.Owner, binding)
		})
	}
	if err == nil {
		err = ctx.Err()
	}
	if err == nil {
		_, err = s.repo.FinishRemoteSync(ctx, job, observation)
		if err == nil {
			s.jobChanged(job.Owner)
		}
	}
	if parent.Err() != nil {
		slog.DebugContext(parent, "Remote playlist task interrupted for shutdown", "jobId", job.ID)
		return
	}
	if err != nil {
		finish, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		failure := &domain.Error{Code: FailureCode(err), Message: "Remote playlist synchronization failed; previous snapshot retained"}
		var limited *domain.Error
		if failure.Code == "RATE_LIMITED" && errors.As(err, &limited) {
			failure.RetryAfter = limited.RetryAfter
		}
		if finishErr := s.repo.FailRemoteSync(finish, job, failure); finishErr != nil {
			current, readErr := s.repo.RemoteSyncJob(finish, job.Owner, job.ID)
			if readErr != nil || current.State == "running" {
				slog.Error("Remote playlist task requires recovery", "jobId", job.ID, "errorCode", FailureCode(finishErr))
			}
			return
		}
		s.jobChanged(job.Owner)
		level, message := slog.LevelWarn, "Remote playlist task failed"
		if failure.Code == "CANCELED" {
			level, message = slog.LevelInfo, "Remote playlist task cancelled"
		}
		slog.Log(finish, level, message, "jobId", job.ID, "errorCode", failure.Code, "durationMs", time.Since(started).Milliseconds())
		return
	}
	slog.InfoContext(ctx, "Remote playlist task completed", "jobId", job.ID, "durationMs", time.Since(started).Milliseconds())
}
