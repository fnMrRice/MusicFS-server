package bootstrap_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/app/bootstrap"
	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/core/logging"
)

func TestManagedLoggingFilesAndRuntimeAPI(t *testing.T) {
	cfg := serverConfig(t)
	base, stop := serve(t, cfg, bootstrap.ServeOptions{LogOutput: io.Discard})
	client := &http.Client{}
	r, err := http.NewRequestWithContext(t.Context(), "POST", base+"/api/v1/auth/session", nil)
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Authorization", "Bearer "+cfg.AdminToken)
	r.Header.Set("Origin", base)
	response, err := client.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	var session auth.Session
	if err := json.NewDecoder(response.Body).Decode(&session); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK || len(response.Cookies()) == 0 {
		t.Fatal("login failed")
	}
	cookie := response.Cookies()[0]
	request, err := http.NewRequestWithContext(t.Context(), "PUT", base+"/api/v1/admin/logging", strings.NewReader(`{"level":"trace","plugins":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	request.AddCookie(cookie)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", base)
	request.Header.Set("X-CSRF-Token", session.CSRFToken)
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatal("logging API is not registered")
	}
	var settings logging.Settings
	if status := readJSON(t, base, "/api/v1/admin/logging", cookie, &settings); status != http.StatusOK || settings.Level != "trace" {
		t.Fatal("runtime log level did not change")
	}
	stop()
	directory := filepath.Join(cfg.DataRoot, "logs")
	server, err := os.ReadFile(filepath.Join(directory, "server.log"))
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"server ready", "HTTP request dispatched", "HTTP request completed", "server stopped"} {
		if !strings.Contains(string(server), text) {
			t.Fatalf("server event missing: %s", text)
		}
	}
	for _, secret := range []string{cfg.AdminToken, session.CSRFToken, cookie.Value} {
		if secret != "" && strings.Contains(string(server), secret) {
			t.Fatal("authentication secret leaked into logs")
		}
	}
	files, err := os.ReadDir(filepath.Join(directory, "plugins"))
	if err != nil || len(files) < 13 {
		t.Fatal("official plugin log files were not created")
	}
	database, err := os.ReadFile(filepath.Join(directory, "plugins", "musicfs.plugin.storage.sqlite.log"))
	if err != nil || !strings.Contains(string(database), "Database ready") {
		t.Fatal("database plugin logs were not routed")
	}
}

func TestLogDirectoryCannotBeSharedByServers(t *testing.T) {
	first := serverConfig(t)
	first.Logging.Directory = t.TempDir()
	_, stop := serve(t, first, bootstrap.ServeOptions{LogOutput: io.Discard})
	second := serverConfig(t)
	second.Logging.Directory = first.Logging.Directory
	err := bootstrap.ServeWithOptions(context.Background(), second, nil, bootstrap.ServeOptions{LogOutput: io.Discard})
	if err == nil || !strings.Contains(err.Error(), "log directory is already in use") {
		t.Fatalf("second server did not reject shared logs: %v", err)
	}
	stop()
	_, stopAgain := serve(t, second, bootstrap.ServeOptions{LogOutput: io.Discard})
	stopAgain()
}
