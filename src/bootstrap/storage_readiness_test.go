package bootstrap

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/src/config"
	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/storage"
)

type readinessStore struct {
	storage.Backend
	available atomic.Bool
}

func (s *readinessStore) Available() bool { return s.available.Load() }

func (s *readinessStore) Close() error {
	s.available.Store(false)
	return s.Backend.Close()
}

func TestStorageAvailabilityControlsHealthReadiness(t *testing.T) {
	for _, monitor := range []bool{false, true} {
		name := "legacy-backend"
		if monitor {
			name = "monitored-backend"
		}
		t.Run(name, func(t *testing.T) {
			driver := sqlite.Driver()
			driver.Name = "health-" + domain.NewID()
			var backend *readinessStore
			driver.Open = func(ctx context.Context, options storage.Options) (storage.Backend, error) {
				store, err := sqlite.Driver().Open(ctx, options)
				if err != nil {
					return nil, err
				}
				backend = &readinessStore{Backend: store}
				backend.available.Store(true)
				if monitor {
					return backend, nil
				}
				return store, nil
			}
			if err := storage.Register(driver); err != nil {
				t.Fatal(err)
			}
			cfg := config.Config{
				ConfigVersion: 1, AdminToken: domain.NewID(),
				DataRoot: t.TempDir(), MediaRoot: t.TempDir(), CacheRoot: t.TempDir(),
				MaxSessions: 8, MaxProcessing: 2, MaxCacheBytes: 8 << 20,
				SessionTTLSeconds: 3600, Storage: config.StorageConfig{Backend: driver.Name},
				Hashing: config.HashingConfig{MaxConcurrent: 1, BytesPerSecond: 4 << 20},
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var lifecycleReady atomic.Bool
			handler, cleanup, err := compose(ctx, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), "readiness", lifecycleReady.Load)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { cancel(); cleanup() }()
			assertHealth := func(path string, status int, wantReady bool) {
				t.Helper()
				recorder := httptest.NewRecorder()
				handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
				var health struct {
					Ready bool `json:"ready"`
				}
				if recorder.Code != status || json.Unmarshal(recorder.Body.Bytes(), &health) != nil || health.Ready != wantReady {
					t.Fatalf("unexpected health response: %d %s", recorder.Code, recorder.Body.String())
				}
			}
			assertHealth("/api/v1/health/ready", http.StatusServiceUnavailable, false)
			lifecycleReady.Store(true)
			assertHealth("/api/v1/health/ready", http.StatusOK, true)
			lifecycleReady.Store(false)
			assertHealth("/api/v1/health/ready", http.StatusServiceUnavailable, false)
			lifecycleReady.Store(true)
			assertHealth("/api/v1/health/ready", http.StatusOK, true)
			if monitor {
				if err = backend.Close(); err != nil {
					t.Fatal(err)
				}
				assertHealth("/api/v1/health/ready", http.StatusServiceUnavailable, false)
				assertHealth("/api/v1/health/live", http.StatusOK, false)
				lifecycleReady.Store(false)
				assertHealth("/api/v1/health/ready", http.StatusServiceUnavailable, false)
			}
		})
	}
}
