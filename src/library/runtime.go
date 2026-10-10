package library

import (
	"context"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/plugincontrol"
)

// Runtime returns a copy of the immutable runtime descriptor. Nested maps and
// configuration pointers are read-only; reconfiguration replaces the descriptor.
func (s *Service) Runtime(id string) (Runtime, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.runtimes[id]
	return r, ok
}
func (s *Service) Runtimes() []Runtime {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]Runtime, 0, len(s.runtimes))
	for _, r := range s.runtimes {
		result = append(result, r)
	}
	return result
}

// UpdateRuntime is called only after the instance gate has stopped admitting
// work and its old operations have drained, or have been forcibly cancelled.
func (s *Service) UpdateRuntime(r Runtime) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous, ok := s.runtimes[r.ID]
	if !ok || previous.Owner != r.Owner {
		return domain.Fail("NOT_FOUND", "Configured provider not found")
	}
	if r.Gate == nil {
		r.Gate = previous.Gate
	}
	s.runtimes[r.ID] = r
	return nil
}
func (s *Service) SetGate(id string, gate *plugincontrol.Gate) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.runtimes[id]
	if !ok {
		return domain.Fail("NOT_FOUND", "Configured provider not found")
	}
	r.Gate = gate
	s.runtimes[id] = r
	return nil
}
func (s *Service) SetProbe(probe Probe) { s.mu.Lock(); s.probe = probe; s.mu.Unlock() }
func (s *Service) SetWriteBackProbeReady(ready func() bool) {
	s.mu.Lock()
	s.writeBackProbeReady = ready
	s.mu.Unlock()
}
func (s *Service) SetProbeSession(factory ProbeSession) {
	s.mu.Lock()
	s.probeSession = factory
	s.mu.Unlock()
}
func (s *Service) beginProbeSession(ctx context.Context) (context.Context, Probe, func(), error) {
	s.mu.RLock()
	factory, probe := s.probeSession, s.probe
	s.mu.RUnlock()
	if factory != nil {
		return factory(ctx)
	}
	return ctx, probe, func() {}, nil
}
func (s *Service) checkProviderWork(ctx context.Context, r Runtime) error { return r.Gate.Check(ctx) }

// WaitIdle observes resource cleanup, not merely context cancellation. Recovery
// barriers are durable intents and do not prevent an idle worker from stopping.
func (s *Service) WaitIdle(ctx context.Context, owner, provider string) error {
	g, err := s.providerGate(owner, provider)
	if err != nil {
		return err
	}
	for {
		g.mu.Lock()
		idle := g.readers == 0 && g.exclusive == nil
		wait := g.changed
		g.mu.Unlock()
		if idle {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-wait:
		}
	}
}
