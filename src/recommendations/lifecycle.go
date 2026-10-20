package recommendations

import (
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/domain"
)

// UpdateRuntime drops only this instance's cached observations and cancels its
// in-flight fetches. Late results cannot republish a retired generation.
func (s *Service) UpdateRuntime(r Runtime) error {
	if r.AccountID != "" || !pluginproto.OnlineIdentity(r.ConfigRevision) {
		return domain.Fail("INVALID_ARGUMENT", "Invalid recommendation runtime")
	}
	r.Name = runtimeName(r)
	s.mu.Lock()
	defer s.mu.Unlock()
	key := runtimeKey{r.Owner, r.InstanceID}
	old, ok := s.runtimes[key]
	if !ok {
		return domain.Fail("NOT_FOUND", "Recommendation runtime not found")
	}
	if r.Platform != old.Platform {
		return domain.Fail("INVALID_ARGUMENT", "Recommendation platform cannot change")
	}
	r.generation = old.generation + 1
	r.epoch = r.Gate.Epoch()
	s.runtimes[key] = r
	for i, current := range s.ordered {
		if current.Owner == r.Owner && current.InstanceID == r.InstanceID {
			s.ordered[i] = r
			break
		}
	}
	for cache, e := range s.entries {
		if cache.owner != r.Owner || cache.instance != r.InstanceID {
			continue
		}
		if e.flight != nil {
			e.flight.abandoned = true
			e.flight.cancel()
		}
		s.bytes -= e.bytes
		delete(s.entries, cache)
	}
	return nil
}
