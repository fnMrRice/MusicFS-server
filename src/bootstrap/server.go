package bootstrap

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/config"
	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/lifecycle"
)

func Serve(ctx context.Context, c config.Config, log *slog.Logger) error {
	return ServeWithOptions(ctx, c, log, ServeOptions{})
}

func ServeWithOptions(ctx context.Context, c config.Config, log *slog.Logger, options ServeOptions) (result error) {
	if err := validateStartupMode(options.StartupMode, c.Listen); err != nil {
		return err
	}
	if options.ShutdownTimeout < 0 {
		return errors.New("shutdown timeout must be positive")
	}
	if err := c.Validate(); err != nil {
		return err
	}
	if _, err := os.Lstat(filepath.Join(c.DataRoot, ".restore-incomplete")); !os.IsNotExist(err) {
		return errors.New("installation restore is incomplete; restore into a new directory")
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
	lockOwned := true
	defer func() {
		if lockOwned {
			_ = lock.Close()
		}
	}()
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
	manager, err := lifecycle.New(lifecycle.Options{Mode: options.StartupMode, InstanceID: instance, LeaseTTL: options.LeaseTTL, StartupTimeout: options.StartupTimeout, Now: options.Now})
	if err != nil {
		return err
	}
	if err = saveStartupRecord(lock, manager); err != nil {
		return err
	}
	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()
	isReady := func() bool { return ready.Load() && manager.Ready() }
	handler, cleanup, err := compose(runCtx, c, log, instance, isReady, manager)
	if err != nil {
		return err
	}
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 32 << 10, BaseContext: func(net.Listener) context.Context { return runCtx }}
	closeAll := cleanup
	defer func() {
		ready.Store(false)
		manager.BeginDrain("shutdown")
		cancelRun()
		lockOwned = false
		if err := finishServer(server, closeAll, lock, manager, options.ShutdownTimeout, options.BeforeCleanup); err != nil {
			result = errors.Join(result, err)
		} else {
			log.Info("server stopped", "instanceId", instance)
		}
	}()
	var web http.Handler
	if c.WebRoot != "" {
		root, err := os.OpenRoot(c.WebRoot)
		if err != nil {
			return errors.New("cannot open Web artifact directory")
		}
		previous := closeAll
		closeAll = func() { previous(); _ = root.Close() }
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
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; media-src 'self'; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		web.ServeHTTP(w, r)
	})
	server.Handler = router
	if err = manager.Activate(); err != nil {
		return err
	}
	ready.Store(true)
	done := make(chan error, 1)
	go func() {
		if c.TLSCert != "" {
			done <- server.ServeTLS(listener, c.TLSCert, c.TLSKey)
		} else {
			done <- server.Serve(listener)
		}
	}()
	log.Info("server ready", "instanceId", instance, "startupMode", manager.Status().StartupMode)
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case err = <-done:
			if !errors.Is(err, http.ErrServerClosed) {
				return err
			}
			return nil
		case <-ctx.Done():
			return nil
		case <-manager.Done():
			return nil
		case <-tick.C:
			manager.Tick()
		}
	}
}
