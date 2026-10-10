package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"os"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/lifecycle"
)

// ServeOptions is trusted process setup. CLI exposes only StartupMode; timing
// overrides and hooks are available to embedding hosts and integration tests.
type ServeOptions struct {
	StartupMode                               string
	LeaseTTL, StartupTimeout, ShutdownTimeout time.Duration
	Now                                       func() time.Time
	BeforeCleanup                             func()
}
type StartupRecord struct {
	InstanceID  string    `json:"instanceId"`
	StartupMode string    `json:"startupMode"`
	PID         int       `json:"pid"`
	StartedAt   time.Time `json:"startedAt"`
}

func validateStartupMode(mode, listen string) error {
	if mode == "" || mode == lifecycle.Standalone {
		return nil
	}
	if mode != lifecycle.ClientManaged {
		return errors.New("startup mode must be standalone or client-managed")
	}
	host, _, err := net.SplitHostPort(listen)
	if err != nil {
		return errors.New("client-managed startup requires a loopback IP listener")
	}
	ip, err := netip.ParseAddr(host)
	if err != nil || !ip.IsLoopback() {
		return errors.New("client-managed startup requires a loopback IP listener")
	}
	return nil
}
func saveStartupRecord(lock *os.File, manager *lifecycle.Manager) error {
	status := manager.Status()
	raw, err := json.Marshal(StartupRecord{InstanceID: status.InstanceID, StartupMode: status.StartupMode, PID: os.Getpid(), StartedAt: status.StartedAt})
	if err != nil {
		return err
	}
	if err = lock.Truncate(0); err != nil {
		return errors.New("cannot persist startup ownership")
	}
	if _, err = lock.WriteAt(append(raw, '\n'), 0); err != nil {
		return errors.New("cannot persist startup ownership")
	}
	if err = lock.Sync(); err != nil {
		return errors.New("cannot persist startup ownership")
	}
	return nil
}

// finishServer transfers the data lock to the cleanup goroutine. A timed-out
// embedding call cannot allow another instance to overlap unfinished writes.
func finishServer(server *http.Server, cleanup func(), lock *os.File, manager *lifecycle.Manager, timeout time.Duration, before func()) error {
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		err := server.Shutdown(ctx)
		if err != nil {
			_ = server.Close()
		}
		if before != nil {
			before()
		}
		cleanup()
		manager.Stopped()
		closeErr := lock.Close()
		if err == nil {
			err = closeErr
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			return errors.New("server shutdown did not complete cleanly")
		}
		return nil
	case <-ctx.Done():
		_ = server.Close()
		return errors.New("server shutdown timed out; data lock retained until cleanup completes")
	}
}
