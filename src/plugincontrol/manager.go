package plugincontrol

import (
	"context"
	"sync"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/config"
	"github.com/fnMrRice/MusicFS-server/src/domain"
)

type runtime struct {
	plugin      config.Plugin
	instance    config.Instance
	gate        *Gate
	hooks       Hooks
	state       string
	generation  int64
	reason      *string
	running     bool
	reconfigure bool
	force       bool
	attempted   bool
	stopFailed  bool
}
type pendingOperation struct {
	Operation
	dirty bool
}
type Manager struct {
	mu                sync.Mutex
	store             *config.Store
	repo              Repository
	desired           config.Config
	runtimes          map[string]*runtime
	pending           map[string]*pendingOperation
	revision, applied string
	ctx               context.Context
	cancel            context.CancelFunc
	done              chan struct{}
	started           bool
	sequence          int64
	subscribers       map[chan Event]struct{}
}

func New(ctx context.Context, desired config.Config, store *config.Store, repo Repository) (*Manager, error) {
	run, cancel := context.WithCancel(ctx)
	m := &Manager{store: store, repo: repo, desired: desired, runtimes: map[string]*runtime{}, pending: map[string]*pendingOperation{}, ctx: run, cancel: cancel, done: make(chan struct{}), subscribers: map[chan Event]struct{}{}}
	if store != nil {
		snap := store.Snapshot()
		m.revision = snap.Revision
	}
	if repo != nil {
		if err := repo.RecoverPluginOperations(ctx); err != nil {
			cancel()
			return nil, err
		}
	}
	for _, p := range desired.Plugins {
		for _, i := range p.Instances {
			m.runtimes[i.InstanceID] = &runtime{plugin: p, instance: i, gate: NewGate(p.CanStart(i)), state: "disabled", generation: 1}
		}
	}
	return m, nil
}
func (m *Manager) Gate(id string) *Gate {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r := m.runtimes[id]; r != nil {
		return r.gate
	}
	return nil
}

// RuntimeConfig supplies composition code with the current process switches.
// Never serialize it through HTTP because it includes deployment paths.
func (m *Manager) RuntimeConfig() config.Config {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := m.desired
	if m.store != nil {
		c = m.store.Current()
	}
	c.Plugins = append([]config.Plugin(nil), c.Plugins...)
	for pi := range c.Plugins {
		p := &c.Plugins[pi]
		p.Instances = append([]config.Instance(nil), p.Instances...)
		for ii := range p.Instances {
			i := &p.Instances[ii]
			if r := m.runtimes[i.InstanceID]; r != nil {
				p.Enabled, i.Enabled = r.plugin.Enabled, r.instance.Enabled
			}
		}
	}
	return c
}

// Register binds an already initialized official runtime before Start.
func (m *Manager) Register(id string, h Hooks, ready bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.runtimes[id]
	if r == nil || m.started {
		return domain.Fail("CONFLICT", "Plugin runtime registration is closed")
	}
	r.hooks = h
	r.running = ready
	r.attempted = true
	if r.plugin.Pending(r.instance) {
		r.state = "pending_configuration"
		code := "CONFIGURATION_REQUIRED"
		r.reason = &code
		r.gate.SetAccepting(false, false)
		return nil
	}
	if r.plugin.CanStart(r.instance) {
		if ready {
			r.state = "ready"
			r.gate.SetAccepting(true, false)
		} else {
			r.state = "failed"
			r.gate.SetUnavailable("SOURCE_OFFLINE", false)
			reason := "START_FAILED"
			r.reason = &reason
		}
	}
	return nil
}
func (m *Manager) Start() {
	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		return
	}
	m.started = true
	m.publishLocked()
	m.mu.Unlock()
	go m.run()
}
func (m *Manager) Close() {
	m.cancel()
	m.mu.Lock()
	started := m.started
	for _, r := range m.runtimes {
		r.gate.SetAccepting(false, true)
	}
	m.mu.Unlock()
	if started {
		<-m.done
	}
}
func (m *Manager) Snapshot() Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := Snapshot{Revision: m.revision, AppliedRevision: m.applied, RestartRequired: m.restartRequiredLocked(), Items: []Plugin{}}
	if m.store != nil {
		v := m.store.Snapshot()
		s.Writable = v.Writable
		s.DiskChanged = v.DiskChanged
		s.RestartRequired = s.RestartRequired || v.DiskChanged
	}
	for _, p := range m.desired.Plugins {
		v := Plugin{PluginID: p.PluginID, ConfiguredEnabled: p.Enabled, Instances: []Instance{}}
		for _, i := range p.Instances {
			r := m.runtimes[i.InstanceID]
			if r == nil {
				code := "RESTART_REQUIRED"
				v.Instances = append(v.Instances, Instance{PluginID: p.PluginID, InstanceID: i.InstanceID, Name: i.Name, Role: i.Role, Capabilities: []string{}, ConfiguredEnabled: i.Enabled, RuntimeState: "disabled", Generation: 1, Reason: &code})
				continue
			}
			capabilities := []string{}
			if r.hooks.Capabilities != nil {
				capabilities = append(capabilities, r.hooks.Capabilities()...)
			}
			reason := r.reason
			if r.plugin.Enabled != p.Enabled || r.instance.Enabled != i.Enabled {
				code := "RESTART_REQUIRED"
				reason = &code
			} else if !r.plugin.Enabled && r.instance.Enabled {
				code := "PARENT_DISABLED"
				reason = &code
			}
			v.Instances = append(v.Instances, Instance{PluginID: p.PluginID, InstanceID: i.InstanceID, Name: i.Name, Role: i.Role, Capabilities: capabilities, ConfiguredEnabled: i.Enabled, EffectiveEnabled: r.plugin.CanStart(r.instance), AcceptingNewWork: r.gate.Accepting(), RuntimeState: r.state, Generation: r.generation, ActiveLeases: r.gate.Active(), Reason: reason})
		}
		s.Items = append(s.Items, v)
	}
	return s
}

// Saved disable switches are applied only when the server starts again.
func (m *Manager) restartRequiredLocked() bool {
	for _, p := range m.desired.Plugins {
		for _, i := range p.Instances {
			r := m.runtimes[i.InstanceID]
			if r == nil || r.plugin.Enabled != p.Enabled || r.instance.Enabled != i.Enabled {
				return true
			}
		}
	}
	return false
}
func (m *Manager) Operation(ctx context.Context, owner, id string) (Operation, error) {
	if m.repo == nil {
		return Operation{}, domain.Fail("NOT_FOUND", "Plugin operation not found")
	}
	return m.repo.GetPluginOperation(ctx, owner, id)
}

func (m *Manager) Subscribe() (chan Event, func()) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ch := make(chan Event, 1)
	m.subscribers[ch] = struct{}{}
	ch <- Event{Revision: m.revision, Sequence: m.sequence}
	return ch, func() { m.mu.Lock(); delete(m.subscribers, ch); m.mu.Unlock() }
}
func (m *Manager) publishLocked() {
	m.sequence++
	for ch := range m.subscribers {
		select {
		case <-ch:
		default:
		}
		select {
		case ch <- Event{Revision: m.revision, Sequence: m.sequence}:
		default:
		}
	}
}
func (m *Manager) run() {
	defer close(m.done)
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-tick.C:
			m.reconcile()
		}
	}
}
