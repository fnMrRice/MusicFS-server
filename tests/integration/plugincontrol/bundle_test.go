package plugincontrol_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/sdk/bundledplugin"
	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugincontrol"
)

func TestDiscoveredPluginPendingConfigureAndDisableSurviveReload(t *testing.T) {
	t.Setenv("MUSICFS_ADMIN_TOKEN", "bundle-test-administrator-credential")
	dir := t.TempDir()
	path := filepath.Join(dir, "server.yaml")
	binary := filepath.Join(dir, "plugins")
	if err := os.WriteFile(binary, []byte("test catalog"), 0600); err != nil {
		t.Fatal(err)
	}
	raw := "configVersion: 1\nallowLoopbackHttp: true\nallowedOrigins: ['http://127.0.0.1:8080']\npluginBundle: './plugins'\n"
	raw += `plugins:
  - pluginId: musicfs.plugin.source.localfiles
    enabled: true
    instances:
      - instanceId: b1548e96-3081-5c9c-8376-bf290e369d51
        enabled: true
        name: Plugin instance
        permissions:
          readDirectories: ['./music']
        config: {}
`
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	repo, err := sqlite.Open(context.Background(), filepath.Join(dir, "state.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	store, err := config.OpenStore(context.Background(), path, nil, repo)
	if err != nil {
		t.Fatal(err)
	}
	m, err := plugincontrol.New(context.Background(), store.Current(), store, repo)
	if err != nil {
		t.Fatal(err)
	}
	var ready atomic.Bool
	var starts atomic.Int32
	if err := m.Register(localInstanceID, plugincontrol.Hooks{
		Ready: ready.Load,
		Start: func(context.Context, config.Plugin, config.Instance) error {
			starts.Add(1)
			ready.Store(true)
			return nil
		},
		Stop: func(context.Context) error { ready.Store(false); return nil },
	}, false); err != nil {
		t.Fatal(err)
	}
	m.Start()
	defer m.Close()
	assertState := func(state string) {
		t.Helper()
		for deadline := time.Now().Add(5 * time.Second); ; {
			for _, p := range m.Snapshot().Items {
				if p.PluginID == "musicfs.plugin.source.localfiles" && p.Instances[0].RuntimeState == state {
					return
				}
			}
			if time.Now().After(deadline) {
				t.Fatalf("expected %s: %+v", state, m.Snapshot())
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	assertState("pending_configuration")
	for _, plugin := range m.Snapshot().Items {
		if plugin.Version == "" || plugin.Version != bundledplugin.Version(plugin.PluginID) {
			t.Fatalf("pending bundle plugin lost its release declaration: %+v", plugin)
		}
	}
	if starts.Load() != 0 {
		t.Fatal("started an unconfigured plugin")
	}
	music := filepath.Join(dir, "music")
	if err := os.Mkdir(music, 0700); err != nil {
		t.Fatal(err)
	}
	roots := []config.RootSettings{{ID: "music", Name: "Music", Path: &music}}
	_, err = m.Change(context.Background(), "admin", config.Change{ExpectedRevision: store.Snapshot().Revision, IdempotencyKey: "configure-bundled-local", PluginID: "musicfs.plugin.source.localfiles", InstanceID: localInstanceID, Settings: &config.InstanceSettings{MusicRoots: &roots}})
	if err != nil {
		t.Fatal(err)
	}
	assertState("ready")
	_, err = m.Change(context.Background(), "admin", config.Change{ExpectedRevision: store.Snapshot().Revision, IdempotencyKey: "disable-bundled-local", PluginID: "musicfs.plugin.source.localfiles", Enabled: false})
	if err != nil {
		t.Fatal(err)
	}
	assertState("ready")
	if !m.Snapshot().RestartRequired || !m.Gate(localInstanceID).Accepting() {
		t.Fatal("saved disable changed the current runtime")
	}
	loaded, err := config.Load(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range loaded.Plugins {
		if p.PluginID == "musicfs.plugin.source.localfiles" && p.Enabled {
			t.Fatal("discovery lost persisted disable")
		}
	}
	body, _ := os.ReadFile(path)
	if !strings.Contains(string(body), "enabled: false") {
		t.Fatal("disable was not saved")
	}
}
