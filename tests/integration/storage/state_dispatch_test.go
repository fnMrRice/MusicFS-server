package storage_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugindb"
	"github.com/fnMrRice/MusicFS-server/src/storage"
)

func TestConfiguredDriverOwnsConfigurationPreloadAndState(t *testing.T) {
	ctx := context.Background()
	driver := sqlite.Driver()
	driver.Name = "registered-state-dispatch"
	reads, opens := 0, 0
	verify := func(cfg plugindb.BackendConfig, plugin, instance string) {
		t.Helper()
		if cfg.Name != driver.Name || cfg.Options["tenant"] != "test" || !filepath.IsAbs(cfg.DataRoot) || plugin != "musicfs.plugin.source.localfiles" || instance != "b1548e96-3081-5c9c-8376-bf290e369d51" {
			t.Fatalf("state factory lost backend or instance scope: %+v, %s, %s", cfg, plugin, instance)
		}
	}
	driver.ReadPluginState = func(_ context.Context, cfg plugindb.BackendConfig, _ string, plugin, instance, key string) (pluginproto.StateValue, error) {
		verify(cfg, plugin, instance)
		reads++
		if key != plugindb.ConfigurationKey {
			return pluginproto.StateValue{}, nil
		}
		value, _ := json.Marshal(config.LocalSettings{Name: "Registered state", MusicRoots: []config.MusicRoot{}})
		return pluginproto.StateValue{Revision: 7, Value: value}, nil
	}
	driver.OpenPluginState = func(_ context.Context, cfg plugindb.BackendConfig, _ string, plugin, instance string) (plugindb.Backend, error) {
		verify(cfg, plugin, instance)
		opens++
		return &driverState{}, nil
	}
	if err := storage.Register(driver); err != nil {
		t.Fatal(err)
	}
	raw := `configVersion: 1
allowLoopbackHttp: true
allowedOrigins: ['http://127.0.0.1:8080']
storage: {backend: registered-state-dispatch, options: {tenant: test}}
plugins:
  - pluginId: musicfs.plugin.source.localfiles
    enabled: false
    instances:
      - instanceId: b1548e96-3081-5c9c-8376-bf290e369d51
        enabled: false
        config: {}
`
	doc, err := config.ParseDocument([]byte(raw), filepath.Join(t.TempDir(), "server.yaml"), map[string]string{"ADMIN_TOKEN": "test-credential-at-least-24-characters"})
	if err != nil {
		t.Fatal(err)
	}
	if reads < 2 || opens != 0 || doc.Config.Plugins[0].Instances[0].Name != "Registered state" {
		t.Fatal("configuration preload bypassed the registered read factory", reads, opens)
	}
	if _, err := os.Stat(config.PluginDatabasePath(doc.Config)); !os.IsNotExist(err) {
		t.Fatal("preload unexpectedly created a SQLite file", err)
	}
	backend, err := plugindb.OpenBackend(ctx, config.PluginDatabaseBackend(doc.Config), config.PluginDatabasePath(doc.Config), "musicfs.plugin.source.localfiles", "b1548e96-3081-5c9c-8376-bf290e369d51")
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	result, err := backend.CompareAndSet(ctx, pluginproto.StateRequest{Key: "settings", Value: json.RawMessage(`"changed"`)})
	if err != nil || result.Revision != 18 || opens != 1 {
		t.Fatal("state writes bypassed the registered driver", result, err)
	}
}
