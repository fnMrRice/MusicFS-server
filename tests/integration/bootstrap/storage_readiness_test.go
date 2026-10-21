package bootstrap_test

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/src/app/bootstrap"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/storage"
)

type readinessStore struct {
	storage.Backend
	available atomic.Bool
}

func (s *readinessStore) Available() bool { return s.available.Load() }

func TestStorageAvailabilityControlsHealthReadiness(t *testing.T) {
	for _, monitor := range []bool{false, true} {
		name := "legacy-backend"
		if monitor {
			name = "monitored-backend"
		}
		t.Run(name, func(t *testing.T) {
			driver := sqlite.Driver()
			driver.Name = "health-" + domain.NewID()
			opened := make(chan *readinessStore, 1)
			driver.Open = func(ctx context.Context, options storage.Options) (storage.Backend, error) {
				store, err := sqlite.Driver().Open(ctx, options)
				if err != nil {
					return nil, err
				}
				backend := &readinessStore{Backend: store}
				backend.available.Store(true)
				opened <- backend
				if monitor {
					return backend, nil
				}
				return store, nil
			}
			if err := storage.Register(driver); err != nil {
				t.Fatal(err)
			}
			cfg := serverConfig(t)
			cfg.Storage.Backend = driver.Name
			base, stop := serve(t, cfg, bootstrap.ServeOptions{})
			defer stop()
			backend := <-opened
			assertHealth := func(path string, status int, ready bool) {
				t.Helper()
				var health struct{ Ready bool }
				got := readJSON(t, base, path, nil, &health)
				if got != status || health.Ready != ready {
					t.Fatalf("unexpected health: status %d ready %v", got, health.Ready)
				}
			}
			assertHealth("/api/v1/health/ready", 200, true)
			if monitor {
				for range 2 {
					backend.available.Store(false)
					assertHealth("/api/v1/health/ready", 503, false)
					assertHealth("/api/v1/health/live", 200, false)
					backend.available.Store(true)
					assertHealth("/api/v1/health/ready", 200, true)
				}
			}
		})
	}
}
