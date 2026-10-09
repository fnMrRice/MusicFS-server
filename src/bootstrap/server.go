package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/fnMrRice/MusicFS-server/src/config"
	"github.com/fnMrRice/MusicFS-server/src/domain"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"
)

func Serve(ctx context.Context, c config.Config, log *slog.Logger) error {
	if err := c.Validate(); err != nil {
		return err
	}
	for _, dir := range []string{c.DataRoot, c.MediaRoot, c.CacheRoot} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return errors.New("cannot prepare writable directory")
		}
	}
	lock, err := os.OpenFile(filepath.Join(c.DataRoot, "server.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return errors.New("cannot open data lock")
	}
	defer lock.Close()
	if err = lockFile(lock); err != nil {
		return errors.New("data directory is already in use")
	}
	listener, err := net.Listen("tcp", c.Listen)
	if err != nil {
		return errors.New("cannot listen on configured address")
	}
	defer listener.Close()
	var ready atomic.Bool
	ready.Store(true)
	instance := domain.NewID()
	mux := http.NewServeMux()
	for _, route := range []string{"GET /api/v1/health/live", "GET /api/v1/health/ready"} {
		mux.HandleFunc(route, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if !ready.Load() {
				w.WriteHeader(503)
			}
			json.NewEncoder(w).Encode(map[string]any{"product": "MusicFS", "instanceId": instance, "apiVersion": APIVersion, "ready": ready.Load()})
		})
	}
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 32 << 10}
	done := make(chan error, 1)
	go func() {
		if c.TLSCert != "" {
			done <- server.ServeTLS(listener, c.TLSCert, c.TLSKey)
		} else {
			done <- server.Serve(listener)
		}
	}()
	log.Info("server ready", "instanceId", instance)
	select {
	case err = <-done:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
	}
	ready.Store(false)
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdown); err != nil {
		server.Close()
		return errors.New("server shutdown timed out")
	}
	log.Info("server stopped")
	return nil
}
