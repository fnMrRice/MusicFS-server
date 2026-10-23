package library

import (
	"context"
	"sync"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugincontrol"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginhost"
	"github.com/fnMrRice/MusicFS-server/src/plugins/sourceaccess"
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
	gate     *providerGate
	once     sync.Once
	ctx      context.Context
	lifetime *plugincontrol.Lease
	runtime  Runtime
}

type readLeaseKey struct{}

func (r *ReadLease) Context() context.Context { return r.WithContext(r.ctx) }
func (r *ReadLease) WithContext(ctx context.Context) context.Context {
	return context.WithValue(plugincontrol.WithLease(ctx, r.lifetime), readLeaseKey{}, r)
}

func (r *ReadLease) Close() error {
	r.once.Do(func() { r.gate.mu.Lock(); r.gate.readers--; r.gate.notify(); r.gate.mu.Unlock(); r.lifetime.Close() })
	return nil
}
func (g *providerGate) notify() { close(g.changed); g.changed = make(chan struct{}) }

func (s *Service) providerGate(owner, provider string) (*providerGate, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
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
	return s.HoldReadLifetime(ctx, ctx, owner, provider)
}

// HoldReadLifetime keeps admission alive for lifetime while writer waits still
// obey the originating request. Sessions use their own bounded lifetime here.
func (s *Service) HoldReadLifetime(ctx, lifetime context.Context, owner, provider string) (*ReadLease, error) {
	g, err := s.providerGate(owner, provider)
	if err != nil {
		return nil, err
	}
	runtime, _ := s.Runtime(provider)
	if runtime.AuthorizeAudio != nil {
		runtime.Gate, err = runtime.AuthorizeAudio(ctx)
		if err != nil {
			return nil, err
		}
	}
	if previous, _ := ctx.Value(readLeaseKey{}).(*ReadLease); previous != nil && previous.runtime.ID == provider {
		lifetime = previous.WithContext(lifetime)
	}
	admission, err := runtime.Gate.Acquire(lifetime)
	if err != nil {
		return nil, err
	}
	run := lifetime
	if admission != nil {
		run = admission.Context()
	}
	acquired := false
	defer func() {
		if !acquired {
			admission.Close()
		}
	}()
	for {
		g.mu.Lock()
		if err = ctx.Err(); err != nil {
			g.mu.Unlock()
			return nil, err
		}
		if err = run.Err(); err != nil {
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
			current, _ := s.Runtime(provider)
			acquired = true
			return &ReadLease{gate: g, ctx: run, lifetime: admission, runtime: current}, nil
		}
		wait := g.changed
		g.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-run.Done():
			return nil, run.Err()
		case <-wait:
		}
	}
}

// holdTask deliberately retains a read even if opening a resource fails. Every
// caller must Revoke the task on all exits, including canceled/failed opens.
func (s *Service) holdTask(ctx context.Context, owner, provider, task string) (*ReadLease, error) {
	if task == "" || len(task) > 128 {
		return nil, domain.Fail("INVALID_ARGUMENT", "Invalid source task")
	}
	g, err := s.providerGate(owner, provider)
	if err != nil {
		return nil, err
	}
	g.mu.Lock()
	existing := g.tasks[task]
	g.mu.Unlock()
	if existing != nil {
		if err := existing.ctx.Err(); err != nil {
			return nil, err
		}
		return existing, nil
	}
	r, err := s.HoldRead(ctx, owner, provider)
	if err != nil {
		return nil, err
	}
	g = r.gate
	g.mu.Lock()
	if g.tasks[task] != nil {
		existing = g.tasks[task]
		g.mu.Unlock()
		r.Close()
		return existing, nil
	}
	g.tasks[task] = r
	g.mu.Unlock()
	return r, nil
}

func (s *Service) taskContext(provider, task string, ctx context.Context) context.Context {
	s.mu.RLock()
	g := s.gates[provider]
	s.mu.RUnlock()
	if g == nil {
		return ctx
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if lease := g.tasks[task]; lease != nil {
		return lease.Context()
	}
	return ctx
}
func (s *Service) revokeTask(provider, task string) {
	s.mu.RLock()
	g := s.gates[provider]
	s.mu.RUnlock()
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
	lifetime        *plugincontrol.Lease
	ctx             context.Context
	runtime         Runtime
}

func (e *ExclusiveLease) Context() context.Context { return e.ctx }

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
	runtime, _ := s.Runtime(provider)
	admission, err := runtime.Gate.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	run := ctx
	if admission != nil {
		run = admission.Context()
	}
	acquired := false
	defer func() {
		if !acquired {
			admission.Close()
		}
	}()
	g.mu.Lock()
	defer g.mu.Unlock()
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if g.readers != 0 || g.exclusive != nil || len(g.barriers) > 1 || (len(g.barriers) == 1 && !g.barriers[jobID]) {
		return nil, domain.Fail("CONFLICT", "Provider is in use or requires writeback recovery")
	}
	current, _ := s.Runtime(provider)
	e := &ExclusiveLease{gate: g, service: s, provider: provider, jobID: jobID, lifetime: admission, ctx: run, runtime: current}
	g.exclusive = e
	acquired = true
	return e, nil
}
func (e *ExclusiveLease) Check(ctx context.Context) error {
	e.gate.mu.Lock()
	defer e.gate.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := e.ctx.Err(); err != nil {
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
		e.lifetime.Close()
	}
	return nil
}
func (e *ExclusiveLease) Access() (*sourceaccess.Access, error) {
	if err := e.Check(context.Background()); err != nil {
		return nil, err
	}
	w := e.runtime.Worker
	if w == nil || w.Access == nil {
		return nil, domain.Fail("SOURCE_OFFLINE", "Provider is offline")
	}
	return w.Access, nil
}

// CheckCapability 校验租约捕获的 Worker，不能以稍后读取的运行配置替代。
func (e *ExclusiveLease) CheckCapability(ctx context.Context, capability string) error {
	if err := e.Check(ctx); err != nil {
		return err
	}
	w := e.runtime.Worker
	if w == nil {
		return domain.Fail("SOURCE_OFFLINE", "Provider is offline")
	}
	if !w.Supports(capability) {
		return domain.Fail("UNSUPPORTED", "Provider does not support the required capability")
	}
	return nil
}

func (e *ExclusiveLease) RebindLocalTrack(ctx context.Context, r pluginhost.LocalRebind) (pluginproto.Observation, error) {
	// Keep the coordination mutex through the short mapping transaction so a
	// concurrent Close cannot let a scan observe its intermediate file states.
	e.gate.mu.Lock()
	defer e.gate.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return pluginproto.Observation{}, err
	}
	if err := e.ctx.Err(); err != nil {
		return pluginproto.Observation{}, err
	}
	if e.closed || e.gate.exclusive != e || r.JobID != e.jobID {
		return pluginproto.Observation{}, domain.Fail("CONFLICT", "Invalid writeback coordination lease")
	}
	w := e.runtime.Worker
	if w == nil {
		return pluginproto.Observation{}, domain.Fail("SOURCE_OFFLINE", "Provider is offline")
	}
	return w.RebindLocalTrack(ctx, r)
}
