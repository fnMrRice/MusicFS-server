package playlists

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

type Service struct {
	repo           Repository
	remote         Remote
	concurrent     int
	mu             sync.Mutex
	running        map[string]context.CancelFunc
	cancel         context.CancelFunc
	wg             sync.WaitGroup
	wake           chan struct{}
	changed        func(string)
	preparePending func(context.Context)
}

// OnJobChanged 在启动工作线程前设置同步任务状态通知。
func (s *Service) OnJobChanged(notify func(string)) { s.changed = notify }

// OnQueueIdle 让第一个工作线程在队列空闲时分批接收尚未入库的歌单。
func (s *Service) OnQueueIdle(prepare func(context.Context)) { s.preparePending = prepare }

func (s *Service) jobChanged(owner string) {
	if s.changed != nil {
		s.changed(owner)
	}
}

func New(repo Repository, remote Remote, maxConcurrent int) (*Service, error) {
	if repo == nil || remote == nil || maxConcurrent < 1 || maxConcurrent > 2 {
		return nil, Invalid()
	}
	return &Service{repo: repo, remote: remote, concurrent: maxConcurrent, running: map[string]context.CancelFunc{}, wake: make(chan struct{}, 1)}, nil
}
func (s *Service) authorize(ctx context.Context, owner string, ref RemoteRef) (Binding, error) {
	if !ValidID(owner) {
		return Binding{}, Invalid()
	}
	if err := ref.Validate(); err != nil {
		return Binding{}, err
	}
	b, err := RetryRemoteRead(ctx, func(ctx context.Context) (Binding, error) {
		return s.remote.Authorize(ctx, owner, ref)
	})
	if err != nil {
		return b, err
	}
	if b.RemoteRef != ref {
		return Binding{}, domain.Fail("ACCESS_DENIED", "Remote authorization changed the requested scope")
	}
	return b, b.Validate()
}

// Import 在同一事务中登记歌单和曲库同步任务。
func (s *Service) Import(ctx context.Context, owner string, ref RemoteRef, key string) (RemotePlaylist, bool, error) {
	b, err := s.authorize(ctx, owner, ref)
	if err != nil {
		return RemotePlaylist{}, false, err
	}
	result, merged, err := s.repo.ImportRemotePlaylist(ctx, owner, b, key)
	if err == nil {
		s.jobChanged(owner)
		s.notify()
	}
	return result, merged, err
}
func (s *Service) Sync(ctx context.Context, owner, id string, expected int64, key string) (SyncJob, bool, error) {
	p, err := s.repo.RemotePlaylist(ctx, owner, id)
	if err != nil {
		return SyncJob{}, false, err
	}
	b, err := s.authorize(ctx, owner, p.RemoteRef)
	if err != nil {
		return SyncJob{}, false, err
	}
	j, merged, err := s.repo.QueueRemoteSync(ctx, owner, id, expected, b, key)
	if err == nil {
		slog.InfoContext(ctx, "Remote playlist task queued", "jobId", j.ID, "merged", merged)
		if !merged {
			s.jobChanged(owner)
		}
		s.notify()
	}
	return j, merged, err
}
func (s *Service) Get(ctx context.Context, owner, id string) (RemotePlaylist, error) {
	return s.repo.RemotePlaylist(ctx, owner, id)
}
func (s *Service) List(ctx context.Context, owner, after string, limit int) ([]RemotePlaylist, error) {
	return s.repo.RemotePlaylists(ctx, owner, after, limit)
}
func (s *Service) GetJob(ctx context.Context, owner, id string) (SyncJob, error) {
	return s.repo.RemoteSyncJob(ctx, owner, id)
}
func (s *Service) Snapshot(ctx context.Context, owner, id string, revision int64, offset, limit int) (SnapshotPage, error) {
	snapshot, err := s.repo.RemoteSnapshot(ctx, owner, id, revision)
	if err != nil {
		return SnapshotPage{}, err
	}
	return Page(snapshot, offset, limit)
}
func (s *Service) Cancel(ctx context.Context, owner, id string) (SyncJob, error) {
	j, err := s.repo.CancelRemoteSync(ctx, owner, id)
	if err == nil {
		slog.InfoContext(ctx, "Remote playlist task cancellation recorded", "jobId", id, "state", j.State)
		s.mu.Lock()
		if cancel := s.running[id]; cancel != nil {
			cancel()
		}
		s.mu.Unlock()
		s.jobChanged(owner)
	}
	return j, err
}
func (s *Service) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		return domain.Fail("CONFLICT", "Remote playlist workers already started")
	}
	if err := s.repo.RecoverRemoteSync(ctx); err != nil {
		return err
	}
	run, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	slog.DebugContext(ctx, "Remote playlist workers started", "concurrency", s.concurrent)
	for i := range s.concurrent {
		s.wg.Add(1)
		go s.worker(run, i == 0)
	}
	return nil
}
func (s *Service) Close() {
	s.mu.Lock()
	cancel := s.cancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
		s.wg.Wait()
	}
}
func (s *Service) notify() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func FailureCode(err error) string {
	var known *domain.Error
	if errors.As(err, &known) {
		switch known.Code {
		case "AUTH_REQUIRED", "PLATFORM_AUTH_REQUIRED", "AUTH_EXPIRED", "ACCESS_DENIED", "NOT_FOUND", "SOURCE_OFFLINE", "PLUGIN_DISABLED", "RATE_LIMITED", "RISK_CONTROL", "RESOURCE_CHANGED", "CONFLICT", "UNSUPPORTED", "CANCELED", "INVALID_ARGUMENT", "PROTOCOL_MISMATCH":
			return known.Code
		}
	}
	if errors.Is(err, context.Canceled) {
		return "CANCELED"
	}
	return "TEMPORARY_FAILURE"
}
