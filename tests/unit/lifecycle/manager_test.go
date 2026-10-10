package lifecycle_test

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/lifecycle"
)

type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time      { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *clock) Add(d time.Duration) { c.mu.Lock(); defer c.mu.Unlock(); c.now = c.now.Add(d) }
func manager(t *testing.T, mode string) (*lifecycle.Manager, *clock) {
	t.Helper()
	c := &clock{now: time.Date(2026, 10, 10, 1, 0, 0, 0, time.UTC)}
	m, e := lifecycle.New(lifecycle.Options{Mode: mode, InstanceID: "instance-one", Now: c.Now})
	if e != nil {
		t.Fatal(e)
	}
	if e = m.Activate(); e != nil {
		t.Fatal(e)
	}
	return m, c
}
func principal(c *clock, id string) lifecycle.Principal {
	return lifecycle.Principal{UserID: "admin", SessionID: id, ExpiresAt: c.Now().Add(time.Hour)}
}
func code(t *testing.T, err error, want string) {
	t.Helper()
	var e *domain.Error
	if !errors.As(err, &e) || e.Code != want {
		t.Fatalf("expected %s, got %v", want, err)
	}
}
func acquire(t *testing.T, m *lifecycle.Manager, p lifecycle.Principal, client string) lifecycle.Lease {
	t.Helper()
	lease, e := m.Acquire(p, lifecycle.AcquireRequest{InstanceID: "instance-one", ClientID: client}, "key-"+client)
	if e != nil {
		t.Fatal(e)
	}
	return lease
}
func active(t *testing.T, m *lifecycle.Manager, want int) {
	t.Helper()
	s := m.Status()
	if s.State != "ready" || s.ActiveLeases != want {
		t.Fatalf("unexpected status: %+v", s)
	}
}

func TestLifecycleMultipleClientsRenewReplayAndLastRelease(t *testing.T) {
	m, c := manager(t, lifecycle.ClientManaged)
	a, b := principal(c, "session-a"), principal(c, "session-b")
	first, second := acquire(t, m, a, "client-one"), acquire(t, m, b, "client-two")
	_, err := m.Release(a, first.ID, lifecycle.ReleaseRequest{InstanceID: "instance-one"})
	if err != nil {
		t.Fatal(err)
	}
	active(t, m, 1)
	c.Add(10 * time.Second)
	renewed, err := m.Renew(b, second.ID, lifecycle.RenewRequest{InstanceID: "instance-one", Sequence: 1})
	if err != nil {
		t.Fatal(err)
	}
	c.Add(5 * time.Second)
	replay, err := m.Renew(b, second.ID, lifecycle.RenewRequest{InstanceID: "instance-one", Sequence: 1})
	if err != nil || !reflect.DeepEqual(replay, renewed) {
		t.Fatal("renewal replay extended the lease", err)
	}
	_, err = m.Renew(b, second.ID, lifecycle.RenewRequest{InstanceID: "instance-one", Sequence: 3})
	code(t, err, "CONFLICT")
	_, err = m.Renew(a, second.ID, lifecycle.RenewRequest{InstanceID: "instance-one", Sequence: 2})
	code(t, err, "NOT_FOUND")
	released, err := m.Release(b, second.ID, lifecycle.ReleaseRequest{InstanceID: "instance-one"})
	if err != nil {
		t.Fatal(err)
	}
	if m.Status().State != "draining" {
		t.Fatal("last lease did not drain")
	}
	select {
	case <-m.Done():
	default:
		t.Fatal("missing shutdown event")
	}
	again, err := m.Release(b, second.ID, lifecycle.ReleaseRequest{InstanceID: "instance-one"})
	if err != nil || !reflect.DeepEqual(released, again) {
		t.Fatal("release is not naturally idempotent", err)
	}
	_, err = m.Acquire(a, lifecycle.AcquireRequest{InstanceID: "instance-one", ClientID: "client-new"}, "new-operation")
	code(t, err, "TEMPORARY_FAILURE")
}

func TestLifecycleTimeoutStandaloneAndClockRollback(t *testing.T) {
	t.Run("first lease deadline", func(t *testing.T) {
		m, c := manager(t, lifecycle.ClientManaged)
		c.Add(30 * time.Second)
		m.Tick()
		if m.Status().State != "draining" || *m.Status().DrainReason != "initialLeaseTimeout" {
			t.Fatal("unclaimed startup stayed resident")
		}
	})
	t.Run("crashed client", func(t *testing.T) {
		m, c := manager(t, lifecycle.ClientManaged)
		p := principal(c, "session-a")
		lease := acquire(t, m, p, "client-one")
		c.Add(30 * time.Second)
		m.Tick()
		_, err := m.Renew(p, lease.ID, lifecycle.RenewRequest{InstanceID: "instance-one", Sequence: 1})
		code(t, err, "SESSION_EXPIRED")
		if m.Status().State != "draining" {
			t.Fatal("expired client stayed resident")
		}
	})
	t.Run("standalone", func(t *testing.T) {
		m, c := manager(t, lifecycle.Standalone)
		c.Add(24 * time.Hour)
		m.Tick()
		active(t, m, 0)
		_, err := m.Acquire(principal(c, "session-a"), lifecycle.AcquireRequest{InstanceID: "instance-one", ClientID: "client-one"}, "operation-one")
		code(t, err, "UNSUPPORTED")
		select {
		case <-m.Done():
			t.Fatal("standalone exited with no leases")
		default:
		}
	})
	t.Run("clock rollback", func(t *testing.T) {
		m, c := manager(t, lifecycle.ClientManaged)
		p := principal(c, "session-a")
		lease := acquire(t, m, p, "client-one")
		c.Add(-time.Hour)
		renewed, err := m.Renew(p, lease.ID, lifecycle.RenewRequest{InstanceID: "instance-one", Sequence: 1})
		if err != nil || !renewed.ExpiresAt.Equal(lease.ExpiresAt) {
			t.Fatal("clock rollback changed monotonic expiry", err)
		}
	})
}

func TestLifecycleRevocationFencesCapturedAuthenticationAndExpiry(t *testing.T) {
	m, c := manager(t, lifecycle.ClientManaged)
	p := principal(c, "session-a")
	other := principal(c, "session-b")
	acquire(t, m, other, "client-two")
	lease := acquire(t, m, p, "client-one")
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = m.Renew(p, lease.ID, lifecycle.RenewRequest{InstanceID: "instance-one", Sequence: 1})
	}()
	go func() { defer wg.Done(); m.RevokeSession(p.SessionID) }()
	wg.Wait()
	_, err := m.Acquire(p, lifecycle.AcquireRequest{InstanceID: "instance-one", ClientID: "client-three"}, "revoked-operation")
	code(t, err, "AUTH_REQUIRED")
	active(t, m, 1)
	_, err = m.Acquire(other, lifecycle.AcquireRequest{InstanceID: "instance-old", ClientID: "client-three"}, "old-instance")
	code(t, err, "CONFLICT")
	short := principal(c, "session-short")
	short.ExpiresAt = c.Now().Add(time.Second)
	shortLease := acquire(t, m, short, "client-short")
	if !shortLease.ExpiresAt.Equal(short.ExpiresAt) {
		t.Fatal("lease outlived authentication")
	}
	c.Add(time.Second)
	m.Tick()
	active(t, m, 1)
}

func TestLifecycleBoundedAcquisitionAndDefensiveStatus(t *testing.T) {
	m, c := manager(t, lifecycle.ClientManaged)
	p := principal(c, "session-a")
	status := m.Status()
	*status.StartupDeadline = c.Now().Add(-time.Hour)
	active(t, m, 0)
	for i := 0; i < lifecycle.MaxLeases; i++ {
		acquire(t, m, p, fmt.Sprintf("client-%04d", i))
	}
	_, err := m.Acquire(p, lifecycle.AcquireRequest{InstanceID: "instance-one", ClientID: "client-overflow"}, "overflow-operation")
	code(t, err, "RATE_LIMITED")
	_, err = m.Acquire(p, lifecycle.AcquireRequest{InstanceID: "instance-one", ClientID: "client-changed"}, "key-client-0000")
	code(t, err, "CONFLICT")
	lease, err := m.Acquire(p, lifecycle.AcquireRequest{InstanceID: "instance-one", ClientID: "client-0000"}, "same-client-new-key")
	if err != nil || lease.ClientID != "client-0000" {
		t.Fatal("existing client did not merge", err)
	}
	active(t, m, lifecycle.MaxLeases)
}
