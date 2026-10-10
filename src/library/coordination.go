package library

import (
	"context"
	"sync"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/pluginhost"
	"github.com/fnMrRice/MusicFS-server/src/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/sourceaccess"
)

type providerGate struct {
	mu        sync.Mutex
	readers   int
	exclusive *ExclusiveLease
	barriers  map[string]bool
	tasks     map[string]*ReadLease
	changed   chan struct{}
}

type ReadLease struct {
	gate *providerGate
	once sync.Once
}

func (r *ReadLease) Close() error {
	r.once.Do(func() { r.gate.mu.Lock(); r.gate.readers--; r.gate.notify(); r.gate.mu.Unlock() })
	return nil
}
func (g *providerGate) notify() { close(g.changed); g.changed = make(chan struct{}) }

func (s *Service) providerGate(owner, provider string) (*providerGate, error) {
	r, ok := s.runtimes[provider]
	if !ok || r.Owner != owner {
		return nil, domain.Fail("NOT_FOUND", "Provider not found")
	}
	return s.gates[provider], nil
}

// HoldRead waits only for an active writer and can be canceled while waiting.
// Once acquired, Close is mandatory: canceling an operation is not proof that
// its file handles and publication callbacks have finished using the source.
func (s *Service) HoldRead(ctx context.Context, owner, provider string) (*ReadLease, error) {
	g, err := s.providerGate(owner, provider)
	if err != nil {
		return nil, err
	}
	for {
		g.mu.Lock()
		if err = ctx.Err(); err != nil {
			g.mu.Unlock()
			return nil, err
		}
		if len(g.barriers) > 0 {
			g.mu.Unlock()
			return nil, domain.Fail("CONFLICT", "Provider has unresolved source writeback")
		}
		if g.exclusive == nil {
			g.readers++
			g.mu.Unlock()
			return &ReadLease{gate: g}, nil
		}
		wait := g.changed
		g.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-wait:
		}
	}
}

// holdTask deliberately retains a read even if opening a resource fails. Every
// caller must Revoke the task on all exits, including canceled/failed opens.
func (s *Service) holdTask(ctx context.Context, owner, provider, task string) error {
	if task == "" || len(task) > 128 {
		return domain.Fail("INVALID_ARGUMENT", "Invalid source task")
	}
	r, err := s.HoldRead(ctx, owner, provider)
	if err != nil {
		return err
	}
	g := r.gate
	g.mu.Lock()
	if g.tasks[task] != nil {
		g.mu.Unlock()
		return r.Close()
	}
	g.tasks[task] = r
	g.mu.Unlock()
	return nil
}
func (s *Service) revokeTask(provider, task string) {
	g := s.gates[provider]
	if g == nil {
		return
	}
	g.mu.Lock()
	r := g.tasks[task]
	delete(g.tasks, task)
	g.mu.Unlock()
	if r != nil {
		r.Close()
	}
}

// BlockProvider restores a persistent recovery barrier before background jobs
// are started. Existing readers may finish, but no new reader can enter.
func (s *Service) BlockProvider(owner, provider, jobID string) error {
	if jobID == "" || len(jobID) > 128 {
		return domain.Fail("INVALID_ARGUMENT", "Invalid writeback barrier")
	}
	g, err := s.providerGate(owner, provider)
	if err != nil {
		return err
	}
	g.mu.Lock()
	g.barriers[jobID] = true
	g.notify()
	g.mu.Unlock()
	return nil
}

// WritebackPending includes the interval after durable commit but before the
// operation has released its source protection. Other jobs do not affect this
// historical result, and a removed runtime has no live coordination barrier.
func (s *Service) WritebackPending(owner, provider, jobID string) bool {
	g, err := s.providerGate(owner, provider)
	if err != nil {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.barriers[jobID] || (g.exclusive != nil && g.exclusive.jobID == jobID)
}

type ExclusiveLease struct {
	gate            *providerGate
	service         *Service
	provider, jobID string
	closed          bool
}

// TryExclusive never waits. Only the owner of the sole recovery barrier may
// enter a blocked provider to finish that exact operation.
func (s *Service) TryExclusive(ctx context.Context, owner, provider, jobID string) (*ExclusiveLease, error) {
	if jobID == "" || len(jobID) > 128 {
		return nil, domain.Fail("INVALID_ARGUMENT", "Invalid writeback operation")
	}
	g, err := s.providerGate(owner, provider)
	if err != nil {
		return nil, err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if g.readers != 0 || g.exclusive != nil || len(g.barriers) > 1 || (len(g.barriers) == 1 && !g.barriers[jobID]) {
		return nil, domain.Fail("CONFLICT", "Provider is in use or requires writeback recovery")
	}
	e := &ExclusiveLease{gate: g, service: s, provider: provider, jobID: jobID}
	g.exclusive = e
	return e, nil
}
func (e *ExclusiveLease) Check(ctx context.Context) error {
	e.gate.mu.Lock()
	defer e.gate.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if e.closed || e.gate.exclusive != e {
		return domain.Fail("CONFLICT", "Writeback coordination lease is closed")
	}
	return nil
}
func (e *ExclusiveLease) Block() error {
	e.gate.mu.Lock()
	defer e.gate.mu.Unlock()
	if e.closed || e.gate.exclusive != e {
		return domain.Fail("CONFLICT", "Writeback coordination lease is closed")
	}
	e.gate.barriers[e.jobID] = true
	e.gate.notify()
	return nil
}
func (e *ExclusiveLease) ClearBarrier() error {
	e.gate.mu.Lock()
	defer e.gate.mu.Unlock()
	if e.closed || e.gate.exclusive != e {
		return domain.Fail("CONFLICT", "Writeback coordination lease is closed")
	}
	delete(e.gate.barriers, e.jobID)
	e.gate.notify()
	return nil
}
func (e *ExclusiveLease) Close() error {
	e.gate.mu.Lock()
	defer e.gate.mu.Unlock()
	if !e.closed {
		e.closed = true
		if e.gate.exclusive == e {
			e.gate.exclusive = nil
		}
		e.gate.notify()
	}
	return nil
}
func (e *ExclusiveLease) Access() (*sourceaccess.Access, error) {
	if err := e.Check(context.Background()); err != nil {
		return nil, err
	}
	w := e.service.runtimes[e.provider].Worker
	if w == nil || w.Access == nil {
		return nil, domain.Fail("SOURCE_OFFLINE", "Provider is offline")
	}
	return w.Access, nil
}
func (e *ExclusiveLease) RebindLocalTrack(ctx context.Context, r pluginhost.LocalRebind) (pluginproto.Observation, error) {
	// Keep the coordination mutex through the short mapping transaction so a
	// concurrent Close cannot let a scan observe its intermediate file states.
	e.gate.mu.Lock()
	defer e.gate.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return pluginproto.Observation{}, err
	}
	if e.closed || e.gate.exclusive != e || r.JobID != e.jobID {
		return pluginproto.Observation{}, domain.Fail("CONFLICT", "Invalid writeback coordination lease")
	}
	w := e.service.runtimes[e.provider].Worker
	if w == nil {
		return pluginproto.Observation{}, domain.Fail("SOURCE_OFFLINE", "Provider is offline")
	}
	return w.RebindLocalTrack(ctx, r)
}
