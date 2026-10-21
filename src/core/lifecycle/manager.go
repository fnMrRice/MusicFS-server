package lifecycle

import (
	"regexp"
	"sync"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

var identifier = regexp.MustCompile(`^[A-Za-z0-9_-]{8,128}$`)

type record struct {
	lease     Lease
	principal Principal
	deadline  time.Time
}
type operation struct {
	request AcquireRequest
	leaseID string
}
type Manager struct {
	mu               sync.Mutex
	options          Options
	state, reason    string
	started, lastNow time.Time
	startupDeadline  *time.Time
	everAcquired     bool
	records          map[string]*record
	operations       map[string]operation
	revoked          map[string]bool
	done             chan struct{}
}

func New(o Options) (*Manager, error) {
	if o.Mode == "" {
		o.Mode = Standalone
	}
	if o.Mode != Standalone && o.Mode != ClientManaged || !identifier.MatchString(o.InstanceID) {
		return nil, domain.Fail("INVALID_ARGUMENT", "Invalid lifecycle startup options")
	}
	if o.LeaseTTL == 0 {
		o.LeaseTTL = DefaultLeaseTTL
	}
	if o.StartupTimeout == 0 {
		o.StartupTimeout = DefaultStartupTimeout
	}
	if o.LeaseTTL < time.Millisecond || o.StartupTimeout < time.Millisecond {
		return nil, domain.Fail("INVALID_ARGUMENT", "Invalid lifecycle timing")
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	now := o.Now()
	return &Manager{options: o, state: "starting", started: now.UTC(), lastNow: now, records: map[string]*record{}, operations: map[string]operation{}, revoked: map[string]bool{}, done: make(chan struct{})}, nil
}
func (m *Manager) nowLocked() time.Time {
	// Keep time.Now's monotonic component for production timeout decisions.
	now := m.options.Now()
	if now.Before(m.lastNow) {
		return m.lastNow
	}
	m.lastNow = now
	return now
}
func (m *Manager) Activate() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state != "starting" {
		return domain.Fail("CONFLICT", "Lifecycle has already started")
	}
	m.state = "ready"
	if m.options.Mode == ClientManaged {
		deadline := m.nowLocked().Add(m.options.StartupTimeout)
		m.startupDeadline = &deadline
	}
	return nil
}
func (m *Manager) activeLocked() int {
	count := 0
	for _, r := range m.records {
		if r.lease.State == "active" {
			count++
		}
	}
	return count
}
func (m *Manager) drainLocked(reason string) {
	if m.state == "draining" || m.state == "stopped" {
		return
	}
	m.state = "draining"
	m.reason = reason
	close(m.done)
}
func (m *Manager) expireLocked(now time.Time) {
	for _, r := range m.records {
		if r.lease.State == "active" && !now.Before(r.deadline) {
			r.lease.State = "expired"
		}
	}
	if m.state != "ready" || m.options.Mode != ClientManaged || m.activeLocked() != 0 {
		return
	}
	if m.everAcquired {
		m.drainLocked("lastLeaseEnded")
	} else if m.startupDeadline != nil && !now.Before(*m.startupDeadline) {
		m.drainLocked("initialLeaseTimeout")
	}
}

// Tick handles crash expiry even while no HTTP request is arriving.
func (m *Manager) Tick()                 { m.mu.Lock(); defer m.mu.Unlock(); m.expireLocked(m.nowLocked()) }
func (m *Manager) Done() <-chan struct{} { return m.done }
func (m *Manager) BeginDrain(reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if reason != "shutdown" && reason != "startupFailure" {
		reason = "shutdown"
	}
	m.drainLocked(reason)
}
func (m *Manager) Stopped() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.drainLocked("shutdown")
	m.state = "stopped"
}
func (m *Manager) Ready() bool { return m.Status().State == "ready" }
func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.expireLocked(m.nowLocked())
	s := Status{InstanceID: m.options.InstanceID, StartupMode: m.options.Mode, State: m.state, ActiveLeases: m.activeLocked(), LeaseTTLMillis: m.options.LeaseTTL.Milliseconds(), StartedAt: m.started}
	if m.startupDeadline != nil && !m.everAcquired {
		value := m.startupDeadline.UTC()
		s.StartupDeadline = &value
	}
	if m.reason != "" {
		value := m.reason
		s.DrainReason = &value
	}
	return s
}
func (m *Manager) principalLocked(p Principal, now time.Time) error {
	if p.UserID == "" || p.SessionID == "" || len(p.UserID) > 128 || len(p.SessionID) > 128 || !now.Before(p.ExpiresAt) || m.revoked[p.SessionID] {
		return domain.Fail("AUTH_REQUIRED", "Lifecycle session expired or revoked")
	}
	return nil
}
func (m *Manager) instanceLocked(id string) error {
	if id != m.options.InstanceID {
		return domain.Fail("CONFLICT", "Server instance changed; discover the server again")
	}
	return nil
}
func (m *Manager) acceptsLocked() error {
	if m.options.Mode != ClientManaged {
		return domain.Fail("UNSUPPORTED", "Standalone servers do not accept lifecycle leases")
	}
	if m.state != "ready" {
		return domain.Fail("TEMPORARY_FAILURE", "Server is draining or not ready; discover the server again")
	}
	return nil
}
func leaseExpiry(now time.Time, p Principal, ttl time.Duration) time.Time {
	expiry := now.Add(ttl)
	if p.ExpiresAt.Before(expiry) {
		// Convert the remaining authentication lifetime into this monotonic
		// clock domain rather than dropping its monotonic component.
		expiry = now.Add(p.ExpiresAt.Sub(now))
	}
	return expiry
}
