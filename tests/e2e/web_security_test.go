package e2e_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/app/bootstrap"
	"github.com/fnMrRice/MusicFS-server/src/core/config"
)

func TestWebPolicyAllowsLoginQRImagesAndPlatformAvatars(t *testing.T) {
	cfg, base := yamlServiceConfig(t)
	cfg.WebRoot = t.TempDir()
	const page = "<!doctype html><title>Login QR policy fixture</title>"
	if err := os.WriteFile(filepath.Join(cfg.WebRoot, "index.html"), []byte(page), 0600); err != nil {
		t.Fatal(err)
	}
	startWebPolicyServer(t, cfg, base)
	c := &client{http: &http.Client{}, base: base}
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		t.Run(method, func(t *testing.T) {
			code, body, header := c.request(t, method, "/", nil, "")
			if code != http.StatusOK || (method == http.MethodGet && string(body) != page) {
				t.Fatalf("Web artifact was not served: %d", code)
			}
			directives := map[string][]string{}
			for directive := range strings.SplitSeq(header.Get("Content-Security-Policy"), ";") {
				fields := strings.Fields(directive)
				if len(fields) > 0 {
					directives[fields[0]] = fields[1:]
				}
			}
			if !slices.Equal(directives["img-src"], []string{"'self'", "data:", "blob:", "https://p1.music.126.net", "https://p2.music.126.net", "https://p3.music.126.net", "https://p4.music.126.net"}) {
				t.Fatal("Web policy must allow same-origin images, inline QR images and verified platform avatar hosts")
			}
			if !slices.Equal(directives["default-src"], []string{"'self'"}) || !slices.Equal(directives["object-src"], []string{"'none'"}) {
				t.Fatal("Login QR images must not relax other resource policies")
			}
			connections := []string{"'self'"}
			for _, origin := range cfg.AllowedOrigins {
				connections = append(connections, strings.Replace(strings.Replace(origin, "https://", "wss://", 1), "http://", "ws://", 1))
			}
			if !slices.Equal(directives["connect-src"], connections) {
				t.Fatal("WebSocket policy must use explicit configured origins")
			}
			if header.Get("X-Content-Type-Options") != "nosniff" {
				t.Fatal("Web artifact MIME protection is missing")
			}
		})
	}
}

// 本测试保留启动日志和最后一次就绪响应，避免静态页面错误被隐藏为启动超时。
func startWebPolicyServer(t *testing.T, cfg config.Config, base string) {
	t.Helper()
	loaded, err := config.Load(writeYAMLConfiguration(t, cfg), map[string]string{"ADMIN_TOKEN": cfg.AdminToken})
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Listen != cfg.Listen || loaded.WebRoot != cfg.WebRoot {
		t.Fatal("Web policy fixture configuration changed while loading")
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	var serveErr error
	go func() {
		defer close(done)
		serveErr = bootstrap.Serve(ctx, loaded, slog.New(slog.NewTextHandler(t.Output(), &slog.HandlerOptions{Level: slog.LevelDebug})))
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
			if serveErr != nil {
				t.Error(serveErr)
			}
		case <-time.After(8 * time.Second):
			t.Error("Web policy server shutdown timeout")
		}
	})
	probe := &http.Client{Timeout: time.Second, Transport: &http.Transport{}}
	t.Cleanup(probe.CloseIdleConnections)
	last := "No readiness response"
	for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); {
		select {
		case <-done:
			t.Fatalf("Web policy server startup failed: %v", serveErr)
		default:
		}
		response, err := probe.Get(base + "/api/v1/health/ready")
		if err == nil {
			body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
			response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return
			}
			last = fmt.Sprintf("status=%d body=%s", response.StatusCode, body)
		} else {
			last = err.Error()
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("Web policy server startup timeout: %s", last)
}
