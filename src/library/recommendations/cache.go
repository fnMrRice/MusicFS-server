package recommendations

import (
	"context"
	"errors"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"time"
)

func (s *Service) get(ctx context.Context, r Runtime, request Request) (result Source) {
	lease, err := r.Gate.AcquireEpoch(ctx, r.epoch)
	if err != nil {
		failure, _ := failureFor(err, s.config.FailureCooldown)
		return failedSource(r, failure.Code, "Recommendation runtime changed or stopped")
	}
	if lease != nil {
		defer lease.Close()
		ctx = lease.Context()
	}
	if provider, ok := r.Provider.(interface{ CheckAvailability(context.Context) error }); ok {
		if err := provider.CheckAvailability(ctx); err != nil {
			code := "SOURCE_OFFLINE"
			var failure *domain.Error
			if errors.As(err, &failure) {
				code = failure.Code
			}
			return failedSource(r, code, "Recommendation instance is unavailable")
		}
	}
	if err := r.Gate.Check(ctx); err != nil {
		code := "CANCELED"
		var failure *domain.Error
		if errors.As(err, &failure) {
			code = failure.Code
		}
		return failedSource(r, code, "Recommendation instance is not accepting new work")
	}
	if provider, ok := r.Provider.(interface {
		AccountScope(context.Context) (string, string, error)
	}); ok {
		account, revision, err := provider.AccountScope(ctx)
		if err != nil {
			failure, _ := failureFor(err, s.config.FailureCooldown)
			return failedSource(r, failure.Code, "Platform login is required")
		}
		r.AccountID = account
		r.ConfigRevision += "\x00" + revision
		defer func() {
			current, generation, err := provider.AccountScope(ctx)
			if err != nil {
				failure, _ := failureFor(err, s.config.FailureCooldown)
				result = failedSource(r, failure.Code, "Platform account is unavailable")
			} else if current != account || generation != revision {
				result = failedSource(r, "PLATFORM_AUTH_REQUIRED", "Platform account changed; sign in and refresh")
			}
		}()
	}
	key := cacheKey{r.Owner, r.InstanceID, r.AccountID, r.ConfigRevision, request.Limit}
	for {
		if ctx.Err() != nil {
			return failedSource(r, "CANCELED", "Recommendation request was canceled")
		}
		s.mu.Lock()
		if current, ok := s.runtimes[runtimeKey{r.Owner, r.InstanceID}]; !ok || current.generation != r.generation {
			s.mu.Unlock()
			return failedSource(r, "RESOURCE_CHANGED", "Recommendation instance changed; refresh sources")
		}
		if s.closed {
			s.mu.Unlock()
			return failedSource(r, "SOURCE_OFFLINE", "Recommendation service is draining")
		}
		now := time.Now()
		e := s.entries[key]
		if e == nil {
			if len(s.entries) >= s.config.MaxEntries && !s.evict(nil) {
				s.mu.Unlock()
				return failedSource(r, "RATE_LIMITED", "Recommendation cache is busy")
			}
			e = &entry{source: sourceFor(r)}
			s.entries[key] = e
		}
		s.clock++
		e.used = s.clock
		sameDay := request.day == "" || request.day == e.day
		if e.flight == nil && ((!request.Refresh && sameDay && e.source.Error == nil && now.Before(e.expires)) || now.Before(e.retry)) {
			result := s.snapshot(e, now)
			s.mu.Unlock()
			return result
		}
		f := e.flight
		if f != nil && (f.abandoned || (request.day != "" && f.day != request.day)) {
			s.mu.Unlock()
			select {
			case <-f.done:
				continue
			case <-ctx.Done():
				return failedSource(r, "CANCELED", "Recommendation request was canceled")
			case <-s.ctx.Done():
				return failedSource(r, "SOURCE_OFFLINE", "Recommendation service is draining")
			}
		}
		if f == nil {
			fetch, cancel := context.WithTimeout(s.ctx, 2*s.config.Timeout)
			lease, err := r.Gate.AcquireEpoch(fetch, r.epoch)
			if err != nil {
				cancel()
				s.mu.Unlock()
				failure, _ := failureFor(err, s.config.FailureCooldown)
				return failedSource(r, failure.Code, "Recommendation runtime changed or stopped")
			}
			if lease != nil {
				fetch = lease.Context()
			}
			day := request.day
			if day == "" {
				day = s.config.Now().Format(time.DateOnly)
			}
			f = &flight{done: make(chan struct{}), cancel: cancel, lease: lease, day: day}
			e.flight = f
			s.wg.Add(1)
			go s.fetch(fetch, key, r, e, f)
		}
		f.waiters++
		s.mu.Unlock()
		defer s.release(f)
		select {
		case <-f.done:
			return cloneSource(f.result)
		case <-ctx.Done():
			return failedSource(r, "CANCELED", "Recommendation request was canceled")
		case <-s.ctx.Done():
			return failedSource(r, "SOURCE_OFFLINE", "Recommendation service is draining")
		}
	}
}

func (s *Service) release(f *flight) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f.waiters--
	if f.waiters == 0 && !f.finished {
		f.abandoned = true
		f.cancel()
	}
}

func (s *Service) snapshot(e *entry, now time.Time) Source {
	result := cloneSource(e.source)
	if result.LastSuccessAt != nil && !now.Before(e.expires) {
		result.Stale = true
	}
	if result.Error != nil && now.Before(e.retry) {
		n := int((e.retry.Sub(now) + time.Second - 1) / time.Second)
		result.Error.RetryAfter = &n
	}
	return result
}

// evict is called under the service mutex and never removes an active fetch.
func (s *Service) evict(exclude *entry) bool {
	var selected cacheKey
	var oldest *entry
	for key, e := range s.entries {
		if e == exclude || e.flight != nil {
			continue
		}
		if oldest == nil || e.used < oldest.used {
			oldest, selected = e, key
		}
	}
	if oldest == nil {
		return false
	}
	delete(s.entries, selected)
	s.bytes -= oldest.bytes
	return true
}

func failedSource(r Runtime, code, message string) Source {
	result := sourceFor(r)
	result.Error = &Failure{Code: code, Message: message}
	return result
}
