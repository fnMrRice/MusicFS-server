package hashworker

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/identification"
)

type Config struct {
	Auto           bool
	MaxConcurrent  int
	BytesPerSecond int64
}

type Service struct {
	repo    identification.HashRepository
	source  identification.HashSource
	config  Config
	pace    limiter
	mu      sync.Mutex
	cancel  context.CancelFunc
	running map[string]context.CancelFunc
	wg      sync.WaitGroup
	started bool
}

func New(repo identification.HashRepository, source identification.HashSource, c Config) (*Service, error) {
	if repo == nil || source == nil || c.MaxConcurrent < 1 || c.MaxConcurrent > 4 || c.BytesPerSecond < 65536 || c.BytesPerSecond > 1<<30 {
		return nil, identification.Invalid()
	}
	return &Service{repo: repo, source: source, config: c, pace: limiter{rate: c.BytesPerSecond}, running: map[string]context.CancelFunc{}}, nil
}

func (s *Service) Submit(ctx context.Context, owner, track, key string) (identification.HashJob, bool, error) {
	target, err := s.source.ResolveHashTarget(ctx, owner, track)
	if err != nil {
		return identification.HashJob{}, false, err
	}
	return s.repo.QueueHash(ctx, owner, target, key, false)
}

func (s *Service) Get(ctx context.Context, owner, id string) (identification.HashJob, error) {
	return s.repo.GetHashJob(ctx, owner, id)
}

func (s *Service) Cancel(ctx context.Context, owner, id string) (identification.HashJob, error) {
	j, err := s.repo.CancelHash(ctx, owner, id)
	if err == nil {
		s.mu.Lock()
		cancel := s.running[id]
		s.mu.Unlock()
		if cancel != nil {
			cancel()
		}
	}
	return j, err
}

func (s *Service) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		return domain.Fail("CONFLICT", "Hash workers already started")
	}
	if err := s.repo.RecoverHashes(ctx); err != nil {
		return err
	}
	run, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.started = true
	for i := 0; i < s.config.MaxConcurrent; i++ {
		s.wg.Add(1)
		go s.worker(run)
	}
	if s.config.Auto {
		s.wg.Add(1)
		go s.auto(run)
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

func pause(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func (s *Service) worker(ctx context.Context) {
	defer s.wg.Done()
	for ctx.Err() == nil {
		j, err := s.repo.ClaimHash(ctx)
		if err != nil {
			wait := 100 * time.Millisecond
			if !errors.Is(err, sql.ErrNoRows) {
				wait = time.Second
			}
			if !pause(ctx, wait) {
				return
			}
			continue
		}
		s.execute(ctx, j)
	}
}

func (s *Service) auto(ctx context.Context) {
	defer s.wg.Done()
	cursors := map[identification.HashScope]string{}
	for ctx.Err() == nil {
		for _, scope := range s.source.AutoHashScopes() {
			items, err := s.repo.HashCandidates(ctx, scope, cursors[scope], 16)
			if err != nil {
				continue
			}
			if len(items) == 0 {
				cursors[scope] = ""
				continue
			}
			for _, target := range items {
				cursors[scope] = target.SourceTrackID
				current, err := s.source.ResolveHashTarget(ctx, scope.Owner, target.SourceTrackID)
				if err != nil || current != target {
					continue
				}
				if _, _, err = s.repo.QueueHash(ctx, scope.Owner, target, "auto-"+target.AssetID, true); err != nil {
					break
				}
			}
		}
		if !pause(ctx, 2*time.Second) {
			return
		}
	}
}
