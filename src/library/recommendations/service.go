package recommendations

import (
	"context"
	"reflect"
	"sync"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

type Service struct {
	mu       sync.Mutex
	config   Config
	runtimes map[runtimeKey]Runtime
	ordered  []Runtime
	entries  map[cacheKey]*entry
	pages    map[string]*pageFlight
	bytes    int64
	clock    uint64
	ctx      context.Context
	cancel   context.CancelFunc
	slots    chan struct{}
	wg       sync.WaitGroup
	closed   bool
	stopped  chan struct{}
}

func New(runtimes []Runtime, config Config) (*Service, error) {
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.MaxEntries == 0 {
		config.MaxEntries = 64
	}
	if config.MaxBytes == 0 {
		config.MaxBytes = 8 << 20
	}
	if config.MaxConcurrent == 0 {
		config.MaxConcurrent = 2
	}
	if config.TTL == 0 {
		config.TTL = 2 * time.Minute
	}
	if config.Timeout == 0 {
		config.Timeout = 10 * time.Second
	}
	if config.FailureCooldown == 0 {
		config.FailureCooldown = 2 * time.Second
	}
	if len(runtimes) > 128 || config.MaxEntries < 1 || config.MaxEntries > 256 || config.MaxBytes < 1<<20 || config.MaxBytes > 64<<20 || config.MaxConcurrent < 1 || config.MaxConcurrent > 4 || config.TTL < time.Millisecond || config.TTL > 2*time.Minute || config.Timeout < time.Millisecond || config.Timeout > 30*time.Second || config.FailureCooldown < time.Millisecond || config.FailureCooldown > time.Minute {
		return nil, domain.Fail("INVALID_ARGUMENT", "Invalid recommendation service limits")
	}
	s := &Service{config: config, runtimes: map[runtimeKey]Runtime{}, entries: map[cacheKey]*entry{}, slots: make(chan struct{}, config.MaxConcurrent), stopped: make(chan struct{})}
	s.pages = make(map[string]*pageFlight)
	for _, r := range runtimes {
		r.generation = 1
		r.epoch = r.Gate.Epoch()
		if r.AccountID != "" {
			return nil, domain.Fail("UNSUPPORTED", "Recommendation account access is not implemented")
		}
		if !pluginproto.OnlineIdentity(r.Owner) || !pluginproto.OnlineIdentity(r.InstanceID) || !pluginproto.OnlineIdentity(r.ConfigRevision) || pluginproto.OnlinePluginID(r.Platform) == "" {
			return nil, domain.Fail("INVALID_ARGUMENT", "Invalid recommendation runtime scope")
		}
		r.Name = runtimeName(r)
		key := runtimeKey{r.Owner, r.InstanceID}
		if _, ok := s.runtimes[key]; ok {
			return nil, domain.Fail("INVALID_ARGUMENT", "Duplicate recommendation runtime")
		}
		if r.Provider != nil {
			value := reflect.ValueOf(r.Provider)
			if value.Kind() == reflect.Pointer && value.IsNil() {
				r.Provider = nil
			}
		}
		s.runtimes[key] = r
		s.ordered = append(s.ordered, r)
	}
	s.ctx, s.cancel = context.WithCancel(context.Background())
	return s, nil
}

func (s *Service) List(ctx context.Context, owner string, request Request) (Response, error) {
	runtimes, err := s.selectRuntimes(ctx, owner, request)
	if err != nil {
		return Response{}, err
	}
	return s.listSources(ctx, runtimes, request)
}

func (s *Service) selectRuntimes(ctx context.Context, owner string, request Request) ([]Runtime, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !pluginproto.OnlineIdentity(owner) || request.Limit < 1 || request.Limit > online.MaxPageItems || len(request.InstanceIDs) > MaxSources {
		return nil, domain.Fail("INVALID_ARGUMENT", "Select up to eight recommendation instances and a limit between 1 and 50")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, domain.Fail("SOURCE_OFFLINE", "Recommendation service is draining")
	}
	runtimes := []Runtime{}
	if len(request.InstanceIDs) == 0 {
		for _, r := range s.ordered {
			if r.Owner == owner {
				runtimes = append(runtimes, r)
			}
		}
	} else {
		seen := map[string]bool{}
		for _, id := range request.InstanceIDs {
			if !pluginproto.OnlineIdentity(id) || seen[id] {
				return nil, domain.Fail("INVALID_ARGUMENT", "Invalid recommendation instance selection")
			}
			seen[id] = true
			r, ok := s.runtimes[runtimeKey{owner, id}]
			if !ok {
				return nil, domain.Fail("NOT_FOUND", "Recommendation instance was not found")
			}
			runtimes = append(runtimes, r)
		}
	}
	if len(runtimes) > MaxSources {
		return nil, domain.Fail("INVALID_ARGUMENT", "Choose at most eight recommendation instances explicitly")
	}
	return runtimes, nil
}

func (s *Service) listSources(ctx context.Context, runtimes []Runtime, request Request) (Response, error) {
	type result struct {
		index  int
		source Source
	}
	results := make(chan result, len(runtimes))
	for i, r := range runtimes {
		go func() {
			source := s.get(ctx, r, request)
			if r.InstanceName != nil {
				if name := r.InstanceName(ctx); name != "" {
					source.Name = name
				}
			}
			results <- result{i, source}
		}()
	}
	response := Response{Sources: make([]Source, len(runtimes))}
	for range runtimes {
		select {
		case r := <-results:
			response.Sources[r.index] = r.source
		case <-ctx.Done():
			return Response{}, ctx.Err()
		case <-s.ctx.Done():
			return Response{}, domain.Fail("SOURCE_OFFLINE", "Recommendation service is draining")
		}
	}
	if err := ctx.Err(); err != nil {
		return Response{}, err
	}
	return response, nil
}

// Shutdown cancels shared fetches and waits for their goroutines. Workers are
// owned by composition, so this service does not close their process handles.
func (s *Service) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	if !s.closed {
		s.closed = true
		s.cancel()
		go func() {
			s.wg.Wait()
			s.mu.Lock()
			s.entries = map[cacheKey]*entry{}
			s.bytes = 0
			s.mu.Unlock()
			close(s.stopped)
		}()
	}
	s.mu.Unlock()
	select {
	case <-s.stopped:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (s *Service) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return s.Shutdown(ctx)
}
