package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/fnMrRice/MusicFS-server/src/app/bootstrap"
	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/playback"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/cookiejar"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

type client struct {
	http       *http.Client
	base, csrf string
}

func (c *client) request(t *testing.T, method, path string, value any, key string) (int, []byte, http.Header) {
	t.Helper()
	var body io.Reader
	if value != nil {
		b, _ := json.Marshal(value)
		body = bytes.NewReader(b)
	}
	r, err := http.NewRequest(method, c.base+path, body)
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Origin", c.base)
	r.Header.Set("X-CSRF-Token", c.csrf)
	r.Header.Set("Content-Type", "application/json")
	if key != "" {
		r.Header.Set("Idempotency-Key", key)
	}
	response, err := c.http.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, raw, response.Header
}
func login(t *testing.T, base, token string) *client {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	c := &client{http: &http.Client{Jar: jar, Timeout: 20 * time.Second}, base: base}
	r, _ := http.NewRequest("POST", base+"/api/v1/auth/session", nil)
	r.Header.Set("Origin", base)
	r.Header.Set("Authorization", "Bearer "+token)
	response, err := c.http.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	raw, _ := io.ReadAll(response.Body)
	if response.StatusCode != 201 && response.StatusCode != 200 {
		t.Fatalf("login %d %s", response.StatusCode, raw)
	}
	testkit.ValidateHTTP(t, "Session", raw)
	var session struct {
		CSRF string `json:"csrfToken"`
	}
	json.Unmarshal(raw, &session)
	c.csrf = session.CSRF
	return c
}
func build(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if runtime.GOOS == "windows" {
		path += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", path, testkit.ProgramPackage(name))
	cmd.Dir = filepath.Join("..", "..")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, output)
	}
	return path
}
func TestFreshServerScanRangePermissionsAndRestart(t *testing.T) {
	dir := t.TempDir()
	music := filepath.Join(dir, "music")
	os.MkdirAll(music, 0700)
	fixture, err := os.ReadFile(filepath.Join("..", "testdata", "audio", "markers.wav"))
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(music, "中文 music.wav")
	os.WriteFile(source, fixture, 0600)
	pluginPath := build(t, "musicfs-plugin-local")
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := l.Addr().String()
	l.Close()
	base := "http://" + address
	cfg := config.Config{ConfigVersion: 1, Listen: address, DataRoot: filepath.Join(dir, "data"), MediaRoot: filepath.Join(dir, "media"), CacheRoot: filepath.Join(dir, "cache"), AdminToken: domain.NewID(), AllowLoopbackHTTP: true, AllowedOrigins: []string{base}, MaxSessions: 8, MaxProcessing: 2, MaxCacheBytes: 64 << 20, SessionTTLSeconds: 3600,
		Hashing: config.HashingConfig{MaxConcurrent: 1, BytesPerSecond: 4 << 20},
		Plugins: []config.Plugin{{PluginID: "musicfs.plugin.source.localfiles", Executable: pluginPath, Enabled: true, Instances: []config.Instance{{InstanceID: "b1548e96-3081-5c9c-8376-bf290e369d51", Name: "Local test", Enabled: true, Config: config.InstanceConfig{MusicRoots: []config.MusicRoot{{ID: "local", Name: "Local test", Path: music}}}}}}}}
	disableLocalLyricsImport(&cfg)
	configPath := writeYAMLConfiguration(t, cfg)
	start := func() func() {
		loaded, err := config.Load(configPath, map[string]string{"ADMIN_TOKEN": cfg.AdminToken})
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- bootstrap.Serve(ctx, loaded, slog.New(slog.NewTextHandler(io.Discard, nil))) }()
		deadline := time.Now().Add(15 * time.Second)
		for {
			r, e := http.Get(base + "/api/v1/health/ready")
			if e == nil {
				r.Body.Close()
				if r.StatusCode == 200 {
					break
				}
			}
			select {
			case err := <-done:
				t.Fatalf("startup failed: %v", err)
			default:
			}
			if time.Now().After(deadline) {
				cancel()
				t.Fatal("startup timeout")
			}
			time.Sleep(20 * time.Millisecond)
		}
		return func() {
			cancel()
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(8 * time.Second):
				t.Fatal("shutdown timeout")
			}
		}
	}
	stop := start()
	stopped := false
	defer func() {
		if !stopped {
			stop()
		}
	}()
	if runtime.GOOS == "windows" {
		command := exec.Command("pwsh", "-NoProfile", "-File", filepath.Join("..", "..", "scripts", "scan.ps1"), "-BaseURL", base, "-ProviderID", "b1548e96-3081-5c9c-8376-bf290e369d51")
		command.Env = append(os.Environ(), "MUSICFS_ADMIN_TOKEN="+cfg.AdminToken)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("scan command failed: %v %s", err, output)
		}
	}
	c := login(t, base, cfg.AdminToken)
	code, raw, _ := c.request(t, "GET", "/api/v1/provider-instances", nil, "")
	if code != 200 {
		t.Fatal(string(raw))
	}
	testkit.ValidateHTTP(t, "Providers", raw)
	code, raw, _ = c.request(t, "POST", "/api/v1/provider-instances/b1548e96-3081-5c9c-8376-bf290e369d51/scan-jobs", providerScanRequest(t, c, "b1548e96-3081-5c9c-8376-bf290e369d51"), "scan-key-0001")
	if code != 202 {
		t.Fatalf("scan %d %s", code, raw)
	}
	var accepted struct {
		ID string `json:"jobId"`
	}
	json.Unmarshal(raw, &accepted)
	for deadline := time.Now().Add(15 * time.Second); ; {
		_, raw, _ = c.request(t, "GET", "/api/v1/scan-jobs/"+accepted.ID, nil, "")
		var j struct {
			Status string `json:"status"`
		}
		json.Unmarshal(raw, &j)
		if j.Status == "succeeded" {
			break
		}
		if j.Status == "failed" || time.Now().After(deadline) {
			t.Fatalf("scan failed %s", raw)
		}
		time.Sleep(20 * time.Millisecond)
	}
	_, raw, _ = c.request(t, "GET", "/api/v1/source-tracks?limit=1", nil, "")
	testkit.ValidateHTTP(t, "SourceTracks", raw)
	var page library.Page
	json.Unmarshal(raw, &page)
	if len(page.Items) != 1 {
		t.Fatalf("unexpected list %s", raw)
	}
	create := playback.CreateRequest{SourceTrackID: page.Items[0].ID, Capabilities: playback.Capabilities{HTMLAudio: true, Formats: []playback.Format{{MIME: "audio/wav; codecs=\"1\"", CanPlay: "probably"}}}}
	code, raw, _ = c.request(t, "POST", "/api/v1/playback/sessions", create, "play-key-0001")
	if code != 201 {
		t.Fatalf("create %d %s", code, raw)
	}
	testkit.ValidateHTTP(t, "Playback", raw)
	var d playback.Description
	json.Unmarshal(raw, &d)
	_, again, _ := c.request(t, "POST", "/api/v1/playback/sessions", create, "play-key-0001")
	if !bytes.Equal(raw, again) {
		t.Fatal("idempotency did not replay")
	}
	verifyRanges(t, c, d, fixture)
	other := login(t, base, cfg.AdminToken)
	code, _, _ = other.request(t, "GET", d.StreamURL, nil, "")
	if code != 404 {
		t.Fatalf("other session read stream: %d", code)
	}
	if err = os.WriteFile(source, []byte("replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	code, raw, _ = c.request(t, "GET", d.StreamURL, nil, "")
	if code != 200 || !bytes.Equal(raw, fixture) {
		t.Fatal("fixed representation was changed")
	}
	code, _, _ = c.request(t, "POST", "/api/v1/playback/sessions", create, "play-key-0002")
	expected := http.StatusConflict
	if runtime.GOOS == "windows" {
		expected = http.StatusUnsupportedMediaType
	}
	if code != expected {
		t.Fatalf("invalid replacement status: got %d, want %d", code, expected)
	}
	code, _, _ = c.request(t, "DELETE", "/api/v1/auth/session", nil, "")
	if code != 204 {
		t.Fatal("logout failed")
	}
	code, _, _ = c.request(t, "GET", d.StreamURL, nil, "")
	if code != 401 {
		t.Fatal("revoked session accepted")
	}
	stop()
	stopped = true
	stop = start()
	stopped = false
	c = login(t, base, cfg.AdminToken)
	_, raw, _ = c.request(t, "GET", "/api/v1/source-tracks", nil, "")
	var after library.Page
	json.Unmarshal(raw, &after)
	if len(after.Items) != 1 || after.Items[0].ID != page.Items[0].ID {
		t.Fatal("library lost across restart")
	}
	code, _, _ = c.request(t, "GET", d.StreamURL, nil, "")
	if code != 404 {
		t.Fatal("old attempt survived restart")
	}
	stop()
	stopped = true
	cfg.Plugins[0].Enabled = false
	cfg.Plugins[0].Executable = filepath.Join(dir, "missing-disabled-worker")
	configPath = writeYAMLConfiguration(t, cfg)
	stop = start()
	stopped = false
	c = login(t, base, cfg.AdminToken)
	_, raw, _ = c.request(t, "GET", "/api/v1/source-tracks", nil, "")
	if json.Unmarshal(raw, &after) != nil || len(after.Items) != 1 || after.Items[0].ID != page.Items[0].ID {
		t.Fatal("disabled provider lost its persisted library")
	}
	_, raw, _ = c.request(t, "GET", "/api/v1/provider-instances", nil, "")
	var providers struct {
		Items []library.Provider `json:"items"`
	}
	if json.Unmarshal(raw, &providers) != nil || len(providers.Items) != 1 || providers.Items[0].ID != "b1548e96-3081-5c9c-8376-bf290e369d51" || providers.Items[0].Availability != "offline" {
		t.Fatalf("disabled provider identity missing: %s", raw)
	}
	if code, _, _ = c.request(t, "POST", "/api/v1/provider-instances/b1548e96-3081-5c9c-8376-bf290e369d51/scan-jobs", providerScanRequest(t, c, "b1548e96-3081-5c9c-8376-bf290e369d51"), "disabled-scan-0001"); code != 503 {
		t.Fatalf("globally disabled provider accepted scan: %d", code)
	}
}
