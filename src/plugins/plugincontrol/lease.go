// Package plugincontrol coordinates desired plugin state and bounded work leases.
package plugincontrol

import (
	"context"
	"sync"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

type Gate struct {
	mu              sync.Mutex
	accepting       bool
	unavailableCode string
	epoch           uint64
	leases          map[*Lease]struct{}
}

type Lease struct {
	gate   *Gate
	ctx    context.Context
	cancel context.CancelFunc
	once   sync.Once
	epoch  uint64
}

type leaseKey struct{}
type leaseContext struct {
	lease    *Lease
	previous *leaseContext
}

// WithLease grants only nested work belonging to an already admitted operation.
// It never changes the original lease's deadline or cancellation authority.
func WithLease(ctx context.Context, lease *Lease) context.Context {
	if lease == nil {
		return ctx
	}
	previous, _ := ctx.Value(leaseKey{}).(*leaseContext)
	return context.WithValue(ctx, leaseKey{}, &leaseContext{lease, previous})
}

func (l *Lease) Context() context.Context { return WithLease(l.ctx, l) }
func (l *Lease) Close() {
	if l == nil {
		return
	}
	l.once.Do(func() {
		l.cancel()
		l.gate.mu.Lock()
		delete(l.gate.leases, l)
		l.gate.mu.Unlock()
	})
}

func (g *Gate) inherited(ctx context.Context) *Lease {
	chain, _ := ctx.Value(leaseKey{}).(*leaseContext)
	for c := chain; c != nil; c = c.previous {
		if c.lease.gate == g && c.lease.ctx.Err() == nil {
			if _, ok := g.leases[c.lease]; ok {
				return c.lease
			}
		}
	}
	return nil
}

func (g *Gate) Check(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if g == nil {
		return nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.accepting && g.inherited(ctx) == nil {
		return g.unavailable()
	}
	return nil
}

func (g *Gate) Acquire(ctx context.Context) (*Lease, error) {
	return g.AcquireEpoch(ctx, 0)
}

// AcquireEpoch rejects a stale runtime object after a worker has been rebuilt.
// Zero is reserved for callers that select their runtime only after admission.
func (g *Gate) AcquireEpoch(ctx context.Context, epoch uint64) (*Lease, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if g == nil {
		return nil, nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	inherited := g.inherited(ctx)
	if !g.accepting && inherited == nil {
		return nil, g.unavailable()
	}
	if epoch != 0 && epoch != g.epoch && (inherited == nil || inherited.epoch != epoch) {
		return nil, domain.Fail("RESOURCE_CHANGED", "Plugin runtime generation changed")
	}
	run, cancel := context.WithCancel(ctx)
	if inherited != nil {
		stop := context.AfterFunc(inherited.ctx, cancel)
		original := cancel
		cancel = func() { stop(); original() }
	}
	l := &Lease{gate: g, ctx: run, cancel: cancel, epoch: g.epoch}
	g.leases[l] = struct{}{}
	return l, nil
}

// NewGate is useful for embedding hosts; the Manager owns production gates.
func NewGate(accepting bool) *Gate {
	return &Gate{accepting: accepting, unavailableCode: "PLUGIN_DISABLED", epoch: 1, leases: map[*Lease]struct{}{}}
}

func (g *Gate) Epoch() uint64 {
	if g == nil {
		return 0
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.epoch
}

// Rotate is called by the runtime owner before constructing a replacement.
func (g *Gate) Rotate() { g.mu.Lock(); g.epoch++; g.mu.Unlock() }

func (g *Gate) Active() int     { g.mu.Lock(); defer g.mu.Unlock(); return len(g.leases) }
func (g *Gate) Accepting() bool { g.mu.Lock(); defer g.mu.Unlock(); return g.accepting }

// SetAccepting is serialized with lease acquisition. Force cancels admitted
// contexts, but their leases remain until resource cleanup explicitly closes them.
func (g *Gate) SetAccepting(accepting, force bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.accepting = accepting
	g.unavailableCode = "PLUGIN_DISABLED"
	g.cancelLocked(force)
}

// SetUnavailable distinguishes a failed desired runtime from an explicit
// disable or drain while retaining the same closed admission boundary.
func (g *Gate) SetUnavailable(code string, force bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.accepting = false
	g.unavailableCode = code
	g.cancelLocked(force)
}

func (g *Gate) unavailable() error {
	if g.unavailableCode == "SOURCE_OFFLINE" {
		return domain.Fail("SOURCE_OFFLINE", "Plugin instance is unavailable")
	}
	return domain.Fail("PLUGIN_DISABLED", "Plugin instance is not accepting new work")
}

func (g *Gate) cancelLocked(force bool) {
	if force {
		for l := range g.leases {
			l.cancel()
		}
	}
}
