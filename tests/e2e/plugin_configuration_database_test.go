package e2e_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestDatabaseConfiguredLocalDirectoryWorkflow(t *testing.T) {
	databaseConfiguredLocalDirectory(t, config.StorageConfig{})
}

func TestPostgresConfiguredLocalDirectoryWorkflow(t *testing.T) {
	databaseConfiguredLocalDirectory(t, config.StorageConfig{Backend: "postgres", Options: testkit.PostgresOptions(t)})
}

func databaseConfiguredLocalDirectory(t *testing.T, storage config.StorageConfig) {
	t.Helper()
	music := t.TempDir()
	payload, err := os.ReadFile(filepath.Join("..", "testdata", "audio", "markers.wav"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(music, "first.wav"), payload, 0600); err != nil {
		t.Fatal(err)
	}
	cfg, base := yamlServiceConfig(t)
	cfg.Storage = storage
	cfg.Plugins = []config.Plugin{{PluginID: "musicfs.plugin.source.localfiles", Executable: build(t, "musicfs-plugin-local"), Enabled: true, Instances: []config.Instance{{InstanceID: "b1548e96-3081-5c9c-8376-bf290e369d51", Enabled: true, Permissions: &config.Permissions{ReadDirectories: []string{music}}}}}}
	disableLocalLyricsImport(&cfg)
	path := writeYAMLConfiguration(t, cfg)
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	stop := startAudioYAMLServer(t, path, cfg, base)
	defer func() { stop() }()
	c := login(t, base, cfg.AdminToken)
	status, raw, _ := c.request(t, "GET", "/api/v1/admin/plugins", nil, "")
	if status != 200 || !bytes.Contains(raw, []byte("pending_configuration")) {
		t.Fatalf("empty provider was not pending: %d %s", status, raw)
	}
	if storage.Backend == "postgres" && !bytes.Contains(raw, []byte("musicfs.plugin.storage.postgres")) {
		t.Fatal("server did not report the configured PostgreSQL backend")
	}
	op := libraryManagementChange(t, c, "/api/v1/admin/plugin-instances/b1548e96-3081-5c9c-8376-bf290e369d51/configuration", map[string]any{"musicRoots": []config.MusicRoot{{ID: "music", Name: "Music", Path: music}}})
	waitLibraryOperation(t, c, op.ID)
	tracks := scanProvider(t, c, "b1548e96-3081-5c9c-8376-bf290e369d51", "database-initial-scan")
	if len(tracks) != 1 {
		t.Fatal("API-selected root was not scanned")
	}
	first := tracks["first.wav"].ID
	saved, _ := os.ReadFile(path)
	if !bytes.Equal(original, saved) {
		t.Fatal("API changed deployment configuration")
	}
	stop()
	stop = startAudioYAMLServer(t, path, cfg, base)
	c = login(t, base, cfg.AdminToken)
	tracks = scanProvider(t, c, "b1548e96-3081-5c9c-8376-bf290e369d51", "database-restored-scan")
	if len(tracks) != 1 || tracks["first.wav"].ID != first {
		t.Fatal("restart lost database root or track identity")
	}
	if storage.Backend == "postgres" {
		for _, path := range []string{"musicfs.sqlite", "providers/plugin-config.sqlite"} {
			if _, err := os.Stat(filepath.Join(cfg.DataRoot, path)); !os.IsNotExist(err) {
				t.Fatalf("unexpected local SQLite file %s: %v", path, err)
			}
		}
	}
}
