package bootstrap_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/src/app/api"
	"github.com/fnMrRice/MusicFS-server/src/app/bootstrap"
	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/storage"
)

func serverConfig(t *testing.T) config.Config {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	return config.Config{ConfigVersion: 1, Listen: address, AdminToken: domain.NewID(), AllowLoopbackHTTP: true, AllowedOrigins: []string{"http://" + address},
		DataRoot: t.TempDir(), MediaRoot: t.TempDir(), CacheRoot: t.TempDir(), MaxSessions: 8, MaxProcessing: 2, MaxCacheBytes: 8 << 20, SessionTTLSeconds: 3600,
		Hashing:        config.HashingConfig{MaxConcurrent: 1, BytesPerSecond: 4 << 20},
		Fingerprinting: config.FingerprintingConfig{MaxConcurrent: 1, TimeoutSeconds: 30},
		WriteBack:      config.WriteBackConfig{MaxFileBytes: 8 << 30}}
}

func serve(t *testing.T, cfg config.Config, options bootstrap.ServeOptions) (string, func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		done <- bootstrap.ServeWithOptions(ctx, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), options)
	}()
	var once sync.Once
	stop := func() {
		once.Do(func() {
			cancel()
			select {
			case err := <-done:
				if err != nil {
					t.Error(err)
				}
			case <-time.After(10 * time.Second):
				t.Error("server did not stop")
			}
		})
	}
	t.Cleanup(stop)
	base := "http://" + cfg.Listen
	client := &http.Client{Timeout: time.Second}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-done:
			once.Do(cancel)
			t.Fatalf("server startup failed: %v", err)
		default:
		}
		response, err := client.Get(base + "/api/v1/health/ready")
		if err == nil {
			response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return base, stop
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("server did not become ready")
	return "", stop
}

func readJSON(t *testing.T, base, path string, cookie *http.Cookie, result any) int {
	t.Helper()
	r, err := http.NewRequestWithContext(t.Context(), http.MethodGet, base+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cookie != nil {
		r.AddCookie(cookie)
	}
	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if err := json.NewDecoder(response.Body).Decode(result); err != nil {
		t.Fatal(err)
	}
	return response.StatusCode
}

// Seed real authenticated sessions through the backend contract so both owners
// exercise the same routing and authorization used by normal server requests.
func withSessions(t *testing.T, cfg *config.Config) map[string]*http.Cookie {
	t.Helper()
	cookies := map[string]*http.Cookie{}
	for _, owner := range []string{"admin", "other-user"} {
		cookies[owner] = &http.Cookie{Name: api.CookieName, Value: domain.NewID()}
	}
	driver := sqlite.Driver()
	driver.Name = "sessions-" + domain.NewID()
	open := driver.Open
	driver.Open = func(ctx context.Context, options storage.Options) (storage.Backend, error) {
		store, err := open(ctx, options)
		if err != nil {
			return nil, err
		}
		for owner, cookie := range cookies {
			_, err = store.(*sqlite.Store).DB.ExecContext(ctx, "INSERT OR IGNORE INTO users(id,role) VALUES(?,?)", owner, "admin")
			if err != nil {
				store.Close()
				return nil, err
			}
			if err = store.DeleteAuth(ctx, auth.Digest(cookie.Value)); err != nil {
				store.Close()
				return nil, err
			}
			err = store.SaveAuth(ctx, auth.Session{ID: auth.Digest(cookie.Value), User: auth.User{ID: owner, Role: "admin"}, CSRFToken: domain.NewID(), ExpiresAt: time.Now().Add(time.Hour)})
			if err != nil {
				store.Close()
				return nil, err
			}
		}
		return store, nil
	}
	if err := storage.Register(driver); err != nil {
		t.Fatal(err)
	}
	cfg.Storage.Backend = driver.Name
	return cookies
}
