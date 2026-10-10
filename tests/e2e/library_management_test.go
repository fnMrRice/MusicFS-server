package e2e_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/config"
	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/playback"
	"github.com/fnMrRice/MusicFS-server/src/plugincontrol"
)

func libraryManagementChange(t *testing.T, c *client, path string, input any) plugincontrol.Operation {
	t.Helper()
	code, raw, _ := c.request(t, "GET", "/api/v1/admin/plugins", nil, "")
	var snapshot plugincontrol.Snapshot
	if code != 200 || json.Unmarshal(raw, &snapshot) != nil || !snapshot.Writable {
		t.Fatalf("plugin manager unavailable: %d %s", code, raw)
	}
	body, _ := json.Marshal(input)
	r, err := http.NewRequest("PUT", c.base+path, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Origin", c.base)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", c.csrf)
	r.Header.Set("Idempotency-Key", domain.NewID())
	r.Header.Set("If-Match", `"`+snapshot.Revision+`"`)
	response, err := c.http.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	raw, err = io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	var operation plugincontrol.Operation
	if response.StatusCode != 202 || json.Unmarshal(raw, &operation) != nil {
		t.Fatalf("plugin change: %d %s", response.StatusCode, raw)
	}
	return operation
}
func waitLibraryOperation(t *testing.T, c *client, id string) {
	t.Helper()
	for deadline := time.Now().Add(12 * time.Second); ; {
		code, raw, _ := c.request(t, "GET", "/api/v1/admin/plugin-operations/"+id, nil, "")
		var operation plugincontrol.Operation
		if code != 200 || json.Unmarshal(raw, &operation) != nil {
			t.Fatalf("plugin operation: %d %s", code, raw)
		}
		if operation.Status == "succeeded" {
			return
		}
		if operation.Status == "failed" || operation.Status == "superseded" || time.Now().After(deadline) {
			t.Fatalf("plugin operation did not finish: %s", raw)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
func waitManagedLibraryTracks(t *testing.T, c *client, count int) map[string]library.Track {
	t.Helper()
	for deadline := time.Now().Add(8 * time.Second); ; {
		code, raw, _ := c.request(t, "GET", "/api/v1/source-tracks", nil, "")
		var page library.Page
		if code != 200 || json.Unmarshal(raw, &page) != nil {
			t.Fatalf("managed library read: %d %s", code, raw)
		}
		tracks := map[string]library.Track{}
		for _, track := range page.Items {
			if track.Availability == "available" {
				tracks[track.DisplayName] = track
			}
		}
		if len(tracks) == count {
			return tracks
		}
		if time.Now().After(deadline) {
			t.Fatalf("watch did not publish %d tracks: %s", count, raw)
		}
		time.Sleep(30 * time.Millisecond)
	}
}

func TestManagedLocalDisableRequiresRestartAndSettingsRebuildWatch(t *testing.T) {
	provider := build(t, "musicfs-plugin-local")
	payload, err := os.ReadFile(filepath.Join("..", "testdata", "audio", "markers.wav"))
	if err != nil {
		t.Fatal(err)
	}
	music := t.TempDir()
	if err = os.WriteFile(filepath.Join(music, "first.wav"), payload, 0600); err != nil {
		t.Fatal(err)
	}
	cfg, base := yamlServiceConfig(t)
	watch := &config.WatchConfig{Enabled: true, DebounceMs: 100, MaxDelayMs: 300, FullRescanSeconds: 30}
	cfg.Plugins = []config.Plugin{{PluginID: "musicfs.localfiles", Enabled: true, Executable: provider, Instances: []config.Instance{{InstanceID: "local", Name: "Music", Role: "provider", Enabled: true, Config: config.InstanceConfig{MusicRoots: []config.MusicRoot{{ID: "music", Name: "Music", Path: music}}, Watch: watch}}}}}
	path := writeYAMLConfiguration(t, cfg)
	stop := startAudioYAMLServer(t, path, cfg, base)
	defer func() { stop() }()
	c := login(t, base, cfg.AdminToken)
	tracks := waitManagedLibraryTracks(t, c, 1)
	first := tracks["first.wav"]
	request := playback.CreateRequest{SourceTrackID: first.ID, Capabilities: playback.Capabilities{HTMLAudio: true, Formats: []playback.Format{{MIME: "audio/wav", CanPlay: "probably"}}}}
	code, raw, _ := c.request(t, "POST", "/api/v1/playback/sessions", request, "managed-old-session")
	var playing playback.Description
	if code != 201 || json.Unmarshal(raw, &playing) != nil {
		t.Fatalf("playback before saved disable: %d %s", code, raw)
	}
	operation := libraryManagementChange(t, c, "/api/v1/admin/plugin-instances/local/state", map[string]any{"enabled": false, "mode": "drain"})
	waitLibraryOperation(t, c, operation.ID)
	if !operation.RestartRequired {
		t.Fatal("saved instance disable omitted restart requirement")
	}
	code, raw, _ = c.request(t, "POST", "/api/v1/playback/sessions", request, "managed-new-session")
	var additional playback.Description
	if code != 201 || json.Unmarshal(raw, &additional) != nil {
		t.Fatalf("pending disable blocked new playback: %d %s", code, raw)
	}
	rangeRequest, _ := http.NewRequest("GET", c.base+playing.StreamURL, nil)
	rangeRequest.Header.Set("Range", "bytes=0-31")
	response, err := c.http.Do(rangeRequest)
	if err != nil {
		t.Fatal(err)
	}
	ranged, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || response.StatusCode != 206 || !bytes.Equal(ranged, payload[:32]) {
		t.Fatalf("old Range failed with pending disable: %d %v", response.StatusCode, err)
	}
	// Original WAV seeks on its byte-range timeline rather than POST timeRestart.
	rangeRequest, _ = http.NewRequest("GET", c.base+playing.StreamURL, nil)
	rangeRequest.Header.Set("Range", "bytes=44-75")
	response, err = c.http.Do(rangeRequest)
	if err != nil {
		t.Fatal(err)
	}
	ranged, err = io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || response.StatusCode != 206 || !bytes.Equal(ranged, payload[44:76]) {
		t.Fatalf("old byte seek failed with pending disable: %d %v", response.StatusCode, err)
	}
	code, raw, _ = c.request(t, "DELETE", "/api/v1/playback/sessions/"+playing.SessionID, nil, "")
	if code != 204 {
		t.Fatalf("session cleanup: %d %s", code, raw)
	}
	code, raw, _ = c.request(t, "DELETE", "/api/v1/playback/sessions/"+additional.SessionID, nil, "")
	if code != 204 {
		t.Fatalf("new session cleanup: %d %s", code, raw)
	}
	code, raw, _ = c.request(t, "GET", "/api/v1/provider-instances", nil, "")
	if code != 200 || !bytes.Contains(raw, []byte(`"availability":"available"`)) {
		t.Fatalf("pending disable changed provider availability: %d %s", code, raw)
	}
	if err = os.WriteFile(filepath.Join(music, "second.wav"), payload, 0600); err != nil {
		t.Fatal(err)
	}
	tracks = waitManagedLibraryTracks(t, c, 2)
	operation = libraryManagementChange(t, c, "/api/v1/admin/plugin-instances/local/state", map[string]any{"enabled": true, "mode": "drain"})
	waitLibraryOperation(t, c, operation.ID)
	if operation.RestartRequired {
		t.Fatal("undoing pending disable still required restart")
	}
	if tracks["first.wav"].ID != first.ID {
		t.Fatal("pending disable changed source identity")
	}
	if tracks = scanProvider(t, c, "local", "managed-explicit-rescan"); len(tracks) != 2 {
		t.Fatal("explicit scan after undoing disable lost tracks", tracks)
	}
	operation = libraryManagementChange(t, c, "/api/v1/admin/plugin-instances/local/configuration", map[string]any{"name": "Updated music", "watch": watch})
	waitLibraryOperation(t, c, operation.ID)
	code, raw, _ = c.request(t, "GET", "/api/v1/admin/plugin-instances/local/configuration", nil, "")
	if code != 200 || !bytes.Contains(raw, []byte("Updated music")) || bytes.Contains(raw, []byte(music)) {
		t.Fatalf("configuration read leaked path or lost settings: %d %s", code, raw)
	}
	// The full reconciliation interval is 30 seconds, so this bound verifies
	// native watch registration or its initial reconciliation after restart.
	if err = os.WriteFile(filepath.Join(music, "third.wav"), payload, 0600); err != nil {
		t.Fatal(err)
	}
	tracks = waitManagedLibraryTracks(t, c, 3)
	if tracks["first.wav"].ID != first.ID {
		t.Fatal("settings update changed stable identity")
	}
	stored, err := os.ReadFile(path)
	if err != nil || strings.Contains(string(stored), "Updated music") {
		t.Fatal("runtime settings changed deployment YAML", err)
	}
	operation = libraryManagementChange(t, c, "/api/v1/admin/plugin-instances/local/state", map[string]any{"enabled": false, "mode": "force"})
	waitLibraryOperation(t, c, operation.ID)
	stop()
	stop = startAudioYAMLServer(t, path, cfg, base)
	c = login(t, base, cfg.AdminToken)
	code, raw, _ = c.request(t, "GET", "/api/v1/admin/plugin-instances/local/configuration", nil, "")
	if code != 200 || !bytes.Contains(raw, []byte("Updated music")) {
		t.Fatalf("database configuration was not restored: %d %s", code, raw)
	}
	code, raw, _ = c.request(t, "POST", "/api/v1/playback/sessions", request, "managed-disabled-after-restart")
	if code != 503 || !bytes.Contains(raw, []byte("PLUGIN_DISABLED")) {
		t.Fatalf("server restart did not apply saved disable: %d %s", code, raw)
	}
	operation = libraryManagementChange(t, c, "/api/v1/admin/plugin-instances/local/state", map[string]any{"enabled": true})
	waitLibraryOperation(t, c, operation.ID)
	tracks = scanProvider(t, c, "local", "database-restart-rescan")
	if len(tracks) != 3 || tracks["first.wav"].ID != first.ID {
		t.Fatal("restart lost configured roots or stable identity")
	}
}
