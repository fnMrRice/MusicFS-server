package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/config"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/plugincontrol"
	"github.com/fnMrRice/MusicFS-server/src/plugindb"
)

func TestDefaultLocalDirectoryPermissionWorkflow(t *testing.T) {
	provider := build(t, "musicfs-plugin-local")
	profile := t.TempDir()
	t.Setenv("USERPROFILE", profile)
	t.Setenv("HOME", profile)
	music := filepath.Join(profile, "Music")
	if err := os.Mkdir(music, 0700); err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(filepath.Join("..", "testdata", "audio", "markers.wav"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(music, "default.wav"), payload, 0600); err != nil {
		t.Fatal(err)
	}
	cfg, base := yamlServiceConfig(t)
	cfg.Plugins = []config.Plugin{{PluginID: "musicfs.plugin.source.localfiles", Executable: provider, Enabled: true, Instances: []config.Instance{{InstanceID: "b1548e96-3081-5c9c-8376-bf290e369d51", Enabled: true}}}}
	// Keep this workflow independent of directories on the test host's drives.
	db, err := plugindb.Open(context.Background(), config.PluginDatabasePath(cfg), "musicfs.plugin.source.localfiles", "local")
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.CompareAndSet(context.Background(), pluginproto.StateRequest{Key: "host-default-read-directories", Value: json.RawMessage(`{"readDirectories":[]}`)})
	closeErr := db.Close()
	if err != nil || closeErr != nil {
		t.Fatalf("initialize isolated directory discovery: %v, %v", err, closeErr)
	}
	path := writeYAMLConfiguration(t, cfg)
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	stop := startAudioYAMLServer(t, path, cfg, base)
	c := login(t, base, cfg.AdminToken)
	assertDefaultLocalPending(t, c)
	code, raw, _ := c.request(t, "GET", "/api/v1/source-tracks", nil, "")
	var initial library.Page
	if code != 200 || json.Unmarshal(raw, &initial) != nil || len(initial.Items) != 0 {
		t.Fatalf("default permission imported tracks before root selection: %d %s", code, raw)
	}
	operation := libraryManagementChange(t, c, "/api/v1/admin/plugin-instances/b1548e96-3081-5c9c-8376-bf290e369d51/configuration", map[string]any{"musicRoots": []config.MusicRoot{{ID: "music", Name: "Music", Path: music}}})
	waitLibraryOperation(t, c, operation.ID)
	tracks := scanProvider(t, c, "b1548e96-3081-5c9c-8376-bf290e369d51", "default-permission-initial-scan")
	first, found := tracks["default.wav"]
	if len(tracks) != 1 || !found {
		t.Fatal("default permission did not allow the selected user Music directory")
	}
	stop()
	stop = startAudioYAMLServer(t, path, cfg, base)
	c = login(t, base, cfg.AdminToken)
	tracks = scanProvider(t, c, "b1548e96-3081-5c9c-8376-bf290e369d51", "default-permission-restored-scan")
	if len(tracks) != 1 || tracks["default.wav"].ID != first.ID {
		t.Fatal("restart lost the selected default directory or track identity")
	}
	operation = libraryManagementChange(t, c, "/api/v1/admin/plugin-instances/b1548e96-3081-5c9c-8376-bf290e369d51/configuration", map[string]any{"musicRoots": []config.MusicRoot{}})
	waitLibraryOperation(t, c, operation.ID)
	assertDefaultLocalPending(t, c)
	stop()
	startAudioYAMLServer(t, path, cfg, base)
	c = login(t, base, cfg.AdminToken)
	assertDefaultLocalPending(t, c)
	saved, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(original, saved) {
		t.Fatal("default permissions or runtime settings changed deployment YAML", err)
	}
}

func assertDefaultLocalPending(t *testing.T, c *client) {
	t.Helper()
	code, raw, _ := c.request(t, "GET", "/api/v1/admin/plugin-instances/b1548e96-3081-5c9c-8376-bf290e369d51/configuration", nil, "")
	var settings plugincontrol.Configuration
	if code != 200 || json.Unmarshal(raw, &settings) != nil || len(settings.Roots) != 0 {
		t.Fatalf("default permission created roots without a selection: %d %s", code, raw)
	}
	code, raw, _ = c.request(t, "GET", "/api/v1/admin/plugins", nil, "")
	if code != 200 || !bytes.Contains(raw, []byte(`"pending_configuration"`)) {
		t.Fatalf("empty local provider was not pending: %d %s", code, raw)
	}
}
