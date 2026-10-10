package recommendations

import (
	"context"
	"time"
)

func (s *Service) get(ctx context.Context, r Runtime, request Request) Source {
	key := cacheKey{r.Owner, r.InstanceID, r.AccountID, r.ConfigRevision, request.Limit}
	for {
		if ctx.Err() != nil {
			return failedSource(r, "CANCELED", "Recommendation request was canceled")
		}
		s.mu.Lock()
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
		if e.flight == nil && ((!request.Refresh && e.source.Error == nil && now.Before(e.expires)) || now.Before(e.retry)) {
			result := s.snapshot(e, now)
			s.mu.Unlock()
			return result
		}
		f := e.flight
		if f != nil && f.abandoned {
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
			f = &flight{done: make(chan struct{}), cancel: cancel}
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
