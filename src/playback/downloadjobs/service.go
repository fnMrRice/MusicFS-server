package downloadjobs

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/playback"
	"github.com/fnMrRice/MusicFS-server/src/playback/onlinecache"
)

const retention = time.Hour
const maxJobs = 4096
const maxItems = 100000

type Service struct {
	dir        string
	source     Source
	cache      Cache
	ctx        context.Context
	cancel     context.CancelFunc
	done       chan struct{}
	wake       chan struct{}
	mu         sync.Mutex
	jobs       map[string]*entry
	operations map[operationKey]*entry
	pending    []*entry
	items      int
	fault      error
}

func Open(ctx context.Context, dir string, source Source, cache Cache) (*Service, error) {
	ctx, cancel := context.WithCancel(ctx)
	s := &Service{dir: dir, source: source, cache: cache, ctx: ctx, cancel: cancel, done: make(chan struct{}), wake: make(chan struct{}, 1), jobs: map[string]*entry{}, operations: map[operationKey]*entry{}}
	if err := s.restore(); err != nil {
		cancel()
		return nil, err
	}
	sort.Slice(s.pending, func(i, j int) bool { return s.pending[i].Created.Before(s.pending[j].Created) })
	go s.run()
	return s, nil
}
func (s *Service) Close() { s.cancel(); <-s.done }

func (s *Service) prior(op operationKey, digest string) (Job, bool, error) {
	if e := s.operations[op]; e != nil {
		if e.Digest != digest {
			return Job{}, true, domain.Fail("CONFLICT", "Idempotency key is bound to a different download request")
		}
		return e.snapshot(), true, nil
	}
	if s.fault != nil || s.ctx.Err() != nil {
		return Job{}, true, domain.Fail("TEMPORARY_FAILURE", "Download scheduler is unavailable")
	}
	return Job{}, false, nil
}

func (s *Service) Submit(ctx context.Context, owner, instance, key string, request Request) (Job, error) {
	if len(key) < 8 || len(key) > 128 || !pluginproto.OnlineIdentity(key) {
		return Job{}, domain.Fail("INVALID_ARGUMENT", "Invalid Idempotency-Key")
	}
	request, err := normalize(request)
	if err != nil {
		return Job{}, err
	}
	op := operationKey{owner, instance, key}
	digest := online.Digest(request)
	s.mu.Lock()
	previous, found, err := s.prior(op, digest)
	s.mu.Unlock()
	if found {
		return previous, err
	}
	binding, err := s.source.DownloadBinding(ctx, owner, instance)
	if err != nil {
		return Job{}, err
	}
	if binding.AccountID != request.AccountID || request.AccountRevision != "" && binding.Revision != request.AccountRevision {
		return Job{}, domain.Fail("RESOURCE_CHANGED", "Selected platform account has changed")
	}
	options, err := s.source.DownloadQualities(ctx, owner, instance)
	if err != nil {
		return Job{}, err
	}
	if !options.Supports(request.Quality) {
		return Job{}, domain.Fail("INVALID_ARGUMENT", "Unsupported download quality")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if prior, found, err := s.prior(op, digest); found {
		return prior, err
	}
	if len(s.jobs) >= maxJobs || s.items+len(request.ExternalIDs) > maxItems {
		return Job{}, domain.Fail("RATE_LIMITED", "Online download queue is at capacity")
	}
	e := newEntry(domain.NewID(), receipt{Operation: op, Digest: digest, Quality: request.Quality, ExternalIDs: request.ExternalIDs, Binding: binding, Created: time.Now().UTC()})
	if err := s.saveNew(e); err != nil {
		return Job{}, err
	}
	s.jobs[e.ID] = e
	s.operations[op] = e
	s.items += e.Total
	s.pending = append(s.pending, e)
	select {
	case s.wake <- struct{}{}:
	default:
	}
	return e.snapshot(), nil
}

func (s *Service) Get(owner, id string) (Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, err := s.find(owner, id)
	if err != nil {
		return Job{}, err
	}
	return e.snapshot(), nil
}

func (s *Service) Summary(owner, id string) (Summary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, err := s.find(owner, id)
	if err != nil {
		return Summary{}, err
	}
	return e.Summary, nil
}

func (s *Service) find(owner, id string) (*entry, error) {
	e := s.jobs[id]
	if e == nil || e.Operation.Owner != owner {
		return nil, domain.Fail("NOT_FOUND", "Online download job not found")
	}
	if s.fault != nil && e.next() != e.Total {
		return nil, domain.Fail("TEMPORARY_FAILURE", "Download scheduler needs recovery")
	}
	return e, nil
}
func (s *Service) expire() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, e := range s.jobs {
		if e.next() != e.Total || time.Since(e.updated) < retention {
			continue
		}
		if err := os.RemoveAll(filepath.Join(s.dir, id)); err != nil {
			continue
		}
		delete(s.jobs, id)
		delete(s.operations, e.Operation)
		s.items -= e.Total
	}
}

func (s *Service) run() {
	defer close(s.done)
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for s.ctx.Err() == nil {
		s.mu.Lock()
		var e *entry
		if len(s.pending) > 0 {
			e = s.pending[0]
			s.pending[0] = nil
			s.pending = s.pending[1:]
			e.State = "running"
		}
		s.mu.Unlock()
		if e == nil {
			select {
			case <-s.ctx.Done():
				return
			case <-s.wake:
			case <-ticker.C:
				s.expire()
			}
			continue
		}
		if err := s.execute(e); err != nil {
			s.mu.Lock()
			s.fault = err
			s.mu.Unlock()
			slog.Error("Online download checkpoint failed", "jobId", e.ID, "error", err)
			return
		}
		select {
		case <-ticker.C:
			s.expire()
		default:
		}
	}
}

func (s *Service) execute(e *entry) error {
	ctx := s.ctx
	idx := e.next()
	item := Item{ExternalID: e.ExternalIDs[idx], State: "succeeded"}
	s.mu.Lock()
	e.Items[idx].State = "running"
	s.mu.Unlock()

	// 尊重平台对连续元数据请求的短暂冷却，整批始终由后台上下文驱动。
	for attempt := 0; ; attempt++ {
		current, err := s.source.DownloadBinding(ctx, e.Operation.Owner, e.Operation.Instance)
		if ctx.Err() != nil {
			return nil
		}
		if err == nil && !e.accepts(current) {
			return s.finishRemaining(e, failure(domain.Fail("RESOURCE_CHANGED", "Online instance or platform account changed")))
		}
		if err != nil {
			code := failure(err).Code
			if code == "PLATFORM_AUTH_REQUIRED" || code == "NOT_FOUND" {
				return s.finishRemaining(e, failure(err))
			}
		} else {
			item.Version, err = s.download(ctx, e, item.ExternalID)
		}
		if ctx.Err() != nil {
			return nil
		}
		if err == nil {
			break
		}
		why := failure(err)
		if why.Code == "AUDIO_CACHE_DISABLED" {
			return s.finishRemaining(e, why)
		}
		if why.Code != "RATE_LIMITED" || why.RetryAfter == nil || *why.RetryAfter < 1 || *why.RetryAfter > 60 || attempt >= 2 {
			item.State = "failed"
			item.Error = why
			break
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(time.Duration(*why.RetryAfter) * time.Second):
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.record(e, []Item{item}); err != nil {
		return err
	}
	// 每次只消费一个条目，其他用户的单曲请求也能在长歌单下载期间获得执行机会。
	if e.next() < e.Total {
		s.pending = append(s.pending, e)
	}
	return nil
}

func (s *Service) download(ctx context.Context, e *entry, externalID string) (*onlinecache.Version, error) {
	ctx = playback.WithDownloadAccount(ctx, e.Binding.AccountID, e.Binding.Revision)
	track, err := s.source.ResolveDownload(ctx, e.Operation.Owner, e.Operation.Instance, externalID, e.Binding)
	if err != nil {
		return nil, err
	}
	version, err := s.cache.DownloadOnlineTrack(ctx, e.Operation.Owner, track.ID, e.Quality)
	if err != nil {
		return nil, err
	}
	return &version, nil
}

func (s *Service) finishRemaining(e *entry, why *Failure) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for e.next() < e.Total && s.ctx.Err() == nil {
		start := e.next()
		items := make([]Item, min(e.Total-start, blockSize-start%blockSize))
		for i := range items {
			items[i] = Item{ExternalID: e.ExternalIDs[start+i], State: "failed", Error: why}
		}
		if err := s.record(e, items); err != nil {
			return err
		}
	}
	return nil
}
