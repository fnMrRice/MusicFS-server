package bootstrap_test

import (
	"bytes"
	"context"
	"io"
	stdlog "log"
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
)

// 在测试替换全局 Logger 前保存原生 Handler，覆盖标准 log 桥接的恢复路径。
var initialBootstrapLogger = slog.Default()

func TestManagedLoggingRejectsConcurrentServersInDifferentDirectories(t *testing.T) {
	first := serverConfig(t)
	_, stopFirst := serve(t, first, bootstrap.ServeOptions{LogOutput: io.Discard})
	second := serverConfig(t)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	err := bootstrap.ServeWithOptions(ctx, second, nil, bootstrap.ServeOptions{LogOutput: io.Discard})
	if err == nil || !strings.Contains(err.Error(), "managed logging is already active in this process") {
		t.Fatalf("Concurrent managed server was not rejected: %v", err)
	}
	slog.Info("Active server logging survived rejected startup")
	stopFirst()
	data, err := os.ReadFile(filepath.Join(first.DataRoot, "logs", "server.log"))
	if err != nil || !bytes.Contains(data, []byte("Active server logging survived rejected startup")) {
		t.Fatal("Rejected startup replaced the active server logger", err)
	}
	_, stopSecond := serve(t, second, bootstrap.ServeOptions{LogOutput: io.Discard})
	stopSecond()
}

func TestManagedLoggingRestoresNativeStandardAndStructuredLogging(t *testing.T) {
	previousLogger := slog.Default()
	previousWriter, previousFlags, previousPrefix := stdlog.Writer(), stdlog.Flags(), stdlog.Prefix()
	t.Cleanup(func() {
		slog.SetDefault(previousLogger)
		stdlog.SetOutput(previousWriter)
		stdlog.SetFlags(previousFlags)
		stdlog.SetPrefix(previousPrefix)
	})
	var output bytes.Buffer
	slog.SetDefault(initialBootstrapLogger)
	stdlog.SetOutput(&output)
	stdlog.SetFlags(stdlog.Ldate | stdlog.Lmsgprefix)
	stdlog.SetPrefix("embedding-host: ")
	_, stop := serve(t, serverConfig(t), bootstrap.ServeOptions{LogOutput: io.Discard})
	stop()
	if slog.Default() != initialBootstrapLogger || stdlog.Writer() != &output || stdlog.Flags() != stdlog.Ldate|stdlog.Lmsgprefix || stdlog.Prefix() != "embedding-host: " {
		t.Fatal("Managed shutdown did not restore the host logging configuration")
	}
	stdlog.Print("Standard logger remains available")
	slog.Info("Structured logger remains available")
	for _, message := range []string{"embedding-host: ", "Standard logger remains available", "Structured logger remains available"} {
		if !strings.Contains(output.String(), message) {
			t.Fatalf("Restored logger dropped output: %s", message)
		}
	}
}

func TestManagedLoggingSurvivesCleanupTimeoutAndReleasesAfterCleanup(t *testing.T) {
	cfg := serverConfig(t)
	ctx, cancel := context.WithCancel(t.Context())
	entered, unblock, returned := make(chan struct{}), make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	release := sync.OnceFunc(func() { close(unblock) })
	t.Cleanup(func() {
		cancel()
		release()
		select {
		case <-returned:
		case <-time.After(5 * time.Second):
			t.Error("Timed-out server did not return")
		}
	})
	go func() {
		done <- bootstrap.ServeWithOptions(ctx, cfg, nil, bootstrap.ServeOptions{
			LogOutput: io.Discard, ShutdownTimeout: 80 * time.Millisecond,
			BeforeCleanup: func() {
				close(entered)
				<-unblock
				slog.Info("Late cleanup diagnostic remains available")
			},
		})
		close(returned)
	}()
	waitManagedLoggingReady(t, cfg, done)
	cancel()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("Cleanup did not reach the blocked phase")
	}
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "server shutdown timed out") {
			t.Fatalf("Expected bounded shutdown timeout: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Server ignored its shutdown timeout")
	}
	slog.Info("Logging remains active while cleanup is blocked")
	path := filepath.Join(cfg.DataRoot, "logs", "server.log")
	data, err := os.ReadFile(path)
	if err != nil || !bytes.Contains(data, []byte("Logging remains active while cleanup is blocked")) {
		t.Fatal("Shutdown timeout closed logging before cleanup completed", err)
	}
	second := serverConfig(t)
	assertManagedLoggingBusy(t, second, "managed logging is already active in this process")
	second.Logging.Directory = filepath.Join(cfg.DataRoot, "logs")
	assertManagedLoggingBusy(t, second, "log directory is already in use")
	release()
	// 数据目录锁在实际清理及日志关闭之后释放，可作为重启的同步边界。
	for deadline := time.Now().Add(5 * time.Second); ; {
		probe, stopProbe := context.WithCancel(context.Background())
		stopProbe()
		err = bootstrap.Serve(probe, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
		if err == nil || !strings.Contains(err.Error(), "data directory is already in use") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Completed cleanup did not release its data lock")
		}
		time.Sleep(10 * time.Millisecond)
	}
	data, err = os.ReadFile(path)
	if err != nil || !bytes.Contains(data, []byte("Late cleanup diagnostic remains available")) || !bytes.Contains(data, []byte("server stopped")) {
		t.Fatal("Final cleanup events were lost", err)
	}
	_, stopRestarted := serve(t, cfg, bootstrap.ServeOptions{LogOutput: io.Discard})
	stopRestarted()
}

func assertManagedLoggingBusy(t *testing.T, cfg config.Config, reason string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	err := bootstrap.ServeWithOptions(ctx, cfg, nil, bootstrap.ServeOptions{LogOutput: io.Discard})
	if err == nil || !strings.Contains(err.Error(), reason) {
		t.Fatalf("Unfinished cleanup released logging ownership: %v", err)
	}
}

func waitManagedLoggingReady(t *testing.T, cfg config.Config, done <-chan error) {
	t.Helper()
	client := &http.Client{Timeout: time.Second}
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); {
		select {
		case err := <-done:
			t.Fatalf("Managed server startup failed: %v", err)
		default:
		}
		response, err := client.Get("http://" + cfg.Listen + "/api/v1/health/ready")
		if err == nil {
			response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("Managed server did not become ready")
}
