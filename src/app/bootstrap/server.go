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

	audioffmpeg "github.com/fnMrRice/MusicFS-server/plugins/decoder/ffmpeg"
	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/core/lifecycle"
	"github.com/fnMrRice/MusicFS-server/src/core/logging"
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
	// 独占目录后、装配任务前回收上次异常退出留下的解码临时文件。
	if err = audioffmpeg.CleanupCodecOutputs(c.DataRoot); err != nil {
		return errors.New("cannot recover codec output storage")
	}
	var logs *logging.Manager
	closeLogs := func() {}
	logsOwned := true
	if options.LogOutput != nil {
		logs, closeLogs, err = startLogging(c, options.LogOutput)
		if err != nil {
			return err
		}
		defer func() {
			if logsOwned {
				closeLogs()
			}
		}()
		log = logs.Logger()
	}
	if log == nil {
		log = slog.Default()
	}
	log.Info("server starting", "startupMode", options.StartupMode)
	defer func() {
		if result != nil && logsOwned {
			log.Error("server stopped with an error", "errorCode", "SERVER_FAILURE")
		}
	}()
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
	if logs != nil {
		handler.RegisterLogging(logs)
	}
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 32 << 10, BaseContext: func(net.Listener) context.Context { return runCtx }}
	closeAll := func() {
		// HTTP Shutdown 不等待已升级的连接；先等待控制命令退出再关闭存储。
		handler.WaitControl()
		cleanup()
	}
	defer func() {
		ready.Store(false)
		manager.BeginDrain("shutdown")
		log.Info("server draining", "instanceId", instance)
		cancelRun()
		handler.CloseControl()
		lockOwned = false
		logsOwned = false
		afterCleanup := func() { log.Info("server stopped", "instanceId", instance); closeLogs() }
		if err := finishServer(server, closeAll, lock, manager, options.ShutdownTimeout, options.BeforeCleanup, afterCleanup); err != nil {
			log.Error("server shutdown incomplete", "errorCode", "SHUTDOWN_INCOMPLETE")
			result = errors.Join(result, err)
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
	webPolicy := webContentPolicy(c.AllowedOrigins)
	router := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api") {
			handler.ServeHTTP(w, r)
			return
		}
		if web == nil || (r.Method != "GET" && r.Method != "HEAD") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Security-Policy", webPolicy)
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
