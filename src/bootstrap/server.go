package bootstrap

import (
	"context"
	"errors"
	"github.com/fnMrRice/MusicFS-server/src/config"
	"github.com/fnMrRice/MusicFS-server/src/domain"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
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
		probe, err := os.CreateTemp(dir, ".write-check-")
		if err != nil {
			return errors.New("configured storage directory is not writable")
		}
		probe.Close()
		os.Remove(probe.Name())
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
	instance := domain.NewID()
	handler, cleanup, err := compose(ctx, c, log, instance, ready.Load)
	if err != nil {
		return err
	}
	defer cleanup()
	var web http.Handler
	if c.WebRoot != "" {
		root, err := os.OpenRoot(c.WebRoot)
		if err != nil {
			return errors.New("cannot open Web artifact directory")
		}
		defer root.Close()
		web = http.FileServerFS(root.FS())
	}
	router := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api") {
			handler.ServeHTTP(w, r)
			return
		}
		if web == nil || (r.Method != "GET" && r.Method != "HEAD") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Security-Policy", "default-src 'self'; media-src 'self'; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		web.ServeHTTP(w, r)
	})
	ready.Store(true)
	server := &http.Server{Handler: router, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 32 << 10}
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
