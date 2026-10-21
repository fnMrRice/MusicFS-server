package e2e_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/app/bootstrap"
	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/core/lifecycle"
)

type lifecycleClock struct {
	mu  sync.Mutex
	now time.Time
}

func lifecycleConfig(t *testing.T) (config.Config, string) {
	t.Helper()
	cfg, base := yamlServiceConfig(t)
	loaded, err := config.Load(writeYAMLConfiguration(t, cfg), map[string]string{"ADMIN_TOKEN": cfg.AdminToken})
	if err != nil {
		t.Fatal(err)
	}
	return loaded, base
}

func (c *lifecycleClock) Now() time.Time      { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *lifecycleClock) Add(d time.Duration) { c.mu.Lock(); defer c.mu.Unlock(); c.now = c.now.Add(d) }

type lifecycleServer struct {
	cancel   context.CancelFunc
	done     chan error
	finished bool
}

func (s *lifecycleServer) wait(t *testing.T) error {
	t.Helper()
	select {
	case err := <-s.done:
		s.finished = true
		return err
	case <-time.After(8 * time.Second):
		t.Fatal("lifecycle shutdown deadline exceeded")
		return nil
	}
}
func startLifecycle(t *testing.T, cfg config.Config, base string, o bootstrap.ServeOptions) *lifecycleServer {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	s := &lifecycleServer{cancel: cancel, done: make(chan error, 1)}
	go func() {
		s.done <- bootstrap.ServeWithOptions(ctx, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), o)
	}()
	t.Cleanup(func() {
		cancel()
		if !s.finished {
			if err := s.wait(t); err != nil {
				t.Error(err)
			}
		}
	})
	for deadline := time.Now().Add(15 * time.Second); ; {
		response, err := http.Get(base + "/api/v1/health/ready")
		if err == nil {
			response.Body.Close()
			if response.StatusCode == 200 {
				return s
			}
		}
		select {
		case err := <-s.done:
			s.finished = true
			t.Fatalf("lifecycle startup failed: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("lifecycle startup deadline exceeded")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
func lifecycleStatus(t *testing.T, c *client) lifecycle.Status {
	t.Helper()
	code, raw, _ := c.request(t, "GET", "/api/v1/lifecycle", nil, "")
	var s lifecycle.Status
	if code != 200 || json.Unmarshal(raw, &s) != nil {
		t.Fatalf("lifecycle status: %d %s", code, raw)
	}
	return s
}
func acquireLifecycle(t *testing.T, c *client, instance, clientID string) lifecycle.Lease {
	t.Helper()
	code, raw, _ := c.request(t, "POST", "/api/v1/lifecycle/leases", lifecycle.AcquireRequest{InstanceID: instance, ClientID: clientID}, "lease-"+clientID)
	var lease lifecycle.Lease
	if code != 201 || json.Unmarshal(raw, &lease) != nil {
		t.Fatalf("acquire lease: %d %s", code, raw)
	}
	return lease
}

func TestLifecycleActualHTTPMultipleClientsAndLogout(t *testing.T) {
	cfg, base := lifecycleConfig(t)
	clock := &lifecycleClock{now: time.Now()}
	s := startLifecycle(t, cfg, base, bootstrap.ServeOptions{StartupMode: lifecycle.ClientManaged, Now: clock.Now})
	a, b := login(t, base, cfg.AdminToken), login(t, base, cfg.AdminToken)
	status := lifecycleStatus(t, a)
	if status.StartupMode != lifecycle.ClientManaged || status.LeaseTTLMillis != 30000 {
		t.Fatal("unexpected startup ownership or public timing")
	}
	first := acquireLifecycle(t, a, status.InstanceID, "client-first")
	acquireLifecycle(t, b, status.InstanceID, "client-second")
	code, raw, _ := a.request(t, "POST", "/api/v1/lifecycle/leases/"+first.ID+"/release", lifecycle.ReleaseRequest{InstanceID: status.InstanceID}, "")
	if code != 200 {
		t.Fatalf("release: %d %s", code, raw)
	}
	if current := lifecycleStatus(t, b); current.ActiveLeases != 1 || current.State != "ready" {
		t.Fatal("one client closed another client server")
	}
	code, raw, _ = b.request(t, "DELETE", "/api/v1/auth/session", nil, "")
	if code != 204 {
		t.Fatalf("logout: %d %s", code, raw)
	}
	if err := s.wait(t); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(cfg.DataRoot, "server.lock"))
	var record bootstrap.StartupRecord
	if err != nil || json.Unmarshal(data, &record) != nil || record.InstanceID != status.InstanceID || record.StartupMode != lifecycle.ClientManaged || record.PID != os.Getpid() {
		t.Fatal("actual startup ownership was not durably recorded", err)
	}
	// An independent restart never inherits the former managed ownership.
	standalone := startLifecycle(t, cfg, base, bootstrap.ServeOptions{Now: clock.Now})
	next := login(t, base, cfg.AdminToken)
	newStatus := lifecycleStatus(t, next)
	if newStatus.StartupMode != lifecycle.Standalone || newStatus.InstanceID == status.InstanceID {
		t.Fatal("restart inherited old process ownership")
	}
	clock.Add(time.Minute)
	time.Sleep(40 * time.Millisecond)
	if lifecycleStatus(t, next).State != "ready" {
		t.Fatal("standalone stopped without clients")
	}
	code, raw, _ = next.request(t, "POST", "/api/v1/lifecycle/leases", lifecycle.AcquireRequest{InstanceID: newStatus.InstanceID, ClientID: "client-third"}, "standalone-lease")
	if code != 415 {
		t.Fatalf("standalone ownership takeover: %d %s", code, raw)
	}
	standalone.cancel()
	if err = standalone.wait(t); err != nil {
		t.Fatal(err)
	}
}

func TestLifecycleActualHTTPLeaseCrashAndUnclaimedStartup(t *testing.T) {
	t.Run("no initial lease", func(t *testing.T) {
		cfg, base := lifecycleConfig(t)
		clock := &lifecycleClock{now: time.Now()}
		s := startLifecycle(t, cfg, base, bootstrap.ServeOptions{StartupMode: lifecycle.ClientManaged, Now: clock.Now})
		clock.Add(30 * time.Second)
		if err := s.wait(t); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("one client crashes", func(t *testing.T) {
		cfg, base := lifecycleConfig(t)
		clock := &lifecycleClock{now: time.Now()}
		s := startLifecycle(t, cfg, base, bootstrap.ServeOptions{StartupMode: lifecycle.ClientManaged, Now: clock.Now})
		a, b := login(t, base, cfg.AdminToken), login(t, base, cfg.AdminToken)
		status := lifecycleStatus(t, a)
		acquireLifecycle(t, a, status.InstanceID, "client-crashed")
		live := acquireLifecycle(t, b, status.InstanceID, "client-running")
		clock.Add(20 * time.Second)
		code, raw, _ := b.request(t, "POST", "/api/v1/lifecycle/leases/"+live.ID+"/renew", lifecycle.RenewRequest{InstanceID: status.InstanceID, Sequence: 1}, "")
		if code != 200 {
			t.Fatalf("renew: %d %s", code, raw)
		}
		clock.Add(10 * time.Second)
		if current := lifecycleStatus(t, b); current.ActiveLeases != 1 || current.State != "ready" {
			t.Fatal("crashed peer ended a live lease")
		}
		clock.Add(20 * time.Second)
		if err := s.wait(t); err != nil {
			t.Fatal(err)
		}
	})
}

func TestLifecycleCleanupTimeoutRetainsDataLock(t *testing.T) {
	cfg, base := lifecycleConfig(t)
	entered, unblock := make(chan struct{}), make(chan struct{})
	var release sync.Once
	defer release.Do(func() { close(unblock) })
	s := startLifecycle(t, cfg, base, bootstrap.ServeOptions{ShutdownTimeout: 80 * time.Millisecond, BeforeCleanup: func() { close(entered); <-unblock }})
	s.cancel()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("cleanup did not start")
	}
	if err := s.wait(t); err == nil || !strings.Contains(err.Error(), "data lock retained") {
		t.Fatal("cleanup timeout was falsely reported as success", err)
	}
	err := bootstrap.Serve(context.Background(), cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err == nil || !strings.Contains(err.Error(), "already in use") {
		t.Fatal("second server overlapped unfinished cleanup", err)
	}
	release.Do(func() { close(unblock) })
	for deadline := time.Now().Add(5 * time.Second); ; {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err = bootstrap.Serve(ctx, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
		if err == nil || !strings.Contains(err.Error(), "already in use") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("completed cleanup did not release its data lock")
		}
		time.Sleep(10 * time.Millisecond)
	}
	next := startLifecycle(t, cfg, base, bootstrap.ServeOptions{})
	next.cancel()
	if err = next.wait(t); err != nil {
		t.Fatal(err)
	}
}

func TestLifecycleManagedRejectsNonLoopbackListener(t *testing.T) {
	cfg, _ := lifecycleConfig(t)
	cfg.Listen = "0.0.0.0:8080"
	err := bootstrap.ServeWithOptions(context.Background(), cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), bootstrap.ServeOptions{StartupMode: lifecycle.ClientManaged})
	if err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Fatal("managed process listened beyond loopback", err)
	}
}
