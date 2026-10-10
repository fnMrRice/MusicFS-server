package playlists

import (
	"context"
	"errors"
	"sync"

	"github.com/fnMrRice/MusicFS-server/src/domain"
)

type Service struct {
	repo       Repository
	remote     Remote
	concurrent int
	mu         sync.Mutex
	running    map[string]context.CancelFunc
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	wake       chan struct{}
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
	b, err := s.remote.Authorize(ctx, owner, ref)
	if err != nil {
		return b, err
	}
	if b.RemoteRef != ref {
		return Binding{}, domain.Fail("ACCESS_DENIED", "Remote authorization changed the requested scope")
	}
	return b, b.Validate()
}

// Import records explicit intent only. Sync is a separate persistent operation;
// simply reading a recommendation or upstream playlist never creates this row.
func (s *Service) Import(ctx context.Context, owner string, ref RemoteRef, key string) (RemotePlaylist, bool, error) {
	b, err := s.authorize(ctx, owner, ref)
	if err != nil {
		return RemotePlaylist{}, false, err
	}
	return s.repo.ImportRemotePlaylist(ctx, owner, b, key)
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
		s.mu.Lock()
		if cancel := s.running[id]; cancel != nil {
			cancel()
		}
		s.mu.Unlock()
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
	for range s.concurrent {
		s.wg.Add(1)
		go s.worker(run)
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
		case "AUTH_REQUIRED", "AUTH_EXPIRED", "ACCESS_DENIED", "NOT_FOUND", "SOURCE_OFFLINE", "RATE_LIMITED", "RESOURCE_CHANGED", "CONFLICT", "UNSUPPORTED", "CANCELED", "INVALID_ARGUMENT":
			return known.Code
		}
	}
	if errors.Is(err, context.Canceled) {
		return "CANCELED"
	}
	return "TEMPORARY_FAILURE"
}
