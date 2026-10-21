package plugincontrol_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugincontrol"
)

func TestLocalSettingsDatabaseAndDirectoryAuthority(t *testing.T) {
	t.Setenv("MUSICFS_ADMIN_TOKEN", "database-test-administrator-credential")
	ctx := context.Background()
	dir, music := t.TempDir(), t.TempDir()
	path := filepath.Join(dir, "server.yaml")
	raw := "configVersion: 1\nallowLoopbackHttp: true\nallowedOrigins: ['http://127.0.0.1:8080']\npluginBundle: ''\nplugins:\n  - pluginId: musicfs.plugin.source.localfiles\n    executable: './worker'\n    enabled: true\n    instances:\n      - instanceId: b1548e96-3081-5c9c-8376-bf290e369d51\n        name: Plugin instance\n        enabled: true\n        permissions:\n          readDirectories: [" + strconv.Quote(music) + "]\n        config: {}\n"
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	repo, err := sqlite.Open(ctx, filepath.Join(dir, "journal.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer repo.Close()
	store, err := config.OpenStore(ctx, path, nil, repo)
	if err != nil {
		t.Fatal(err)
	}
	m, err := plugincontrol.New(ctx, store.Current(), store, repo)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	initial := store.Snapshot().Revision
	roots := []config.RootSettings{{ID: "music", Name: "Music", Path: &music}}
	change := config.Change{PluginID: "musicfs.plugin.source.localfiles", InstanceID: localInstanceID, ExpectedRevision: initial, IdempotencyKey: "database-configure", Settings: &config.InstanceSettings{MusicRoots: &roots}}
	op, err := m.Change(ctx, "admin", change)
	if err != nil {
		t.Fatal(err)
	}
	again, err := m.Change(ctx, "admin", change)
	if err != nil || again.ID != op.ID {
		t.Fatalf("idempotency: %+v %v", again, err)
	}
	stored, _ := os.ReadFile(path)
	if !bytes.Equal(stored, []byte(raw)) {
		t.Fatal("settings changed deployment YAML")
	}
	if store.Snapshot().Revision == initial || store.Snapshot().DiskChanged {
		t.Fatal("invalid composite configuration revision")
	}
	loaded, err := config.Load(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Plugins[0].Instances[0].Config.MusicRoots[0].Path != music {
		t.Fatal("database settings were not restored")
	}
	// Deployment revocation must stop the instance without deleting its settings.
	revoked := strings.Replace(raw, "["+strconv.Quote(music)+"]", "[]", 1)
	if err := os.WriteFile(path, []byte(revoked), 0600); err != nil {
		t.Fatal(err)
	}
	loaded, err = config.Load(path, nil)
	if err != nil || !loaded.Plugins[0].Instances[0].PendingConfiguration || len(loaded.Plugins[0].Instances[0].Config.MusicRoots) != 1 {
		t.Fatalf("revocation lost settings or left provider active: %v", err)
	}
	// Writeback can refer to an API-created root without copying it into YAML.
	granted := raw + "writeBack: {roots: [{instanceId: b1548e96-3081-5c9c-8376-bf290e369d51, rootId: music, fields: [lyrics]}]}\n"
	if err := os.WriteFile(path, []byte(granted), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Load(path, nil); err != nil {
		t.Fatalf("stored roots unavailable to writeback validation: %v", err)
	}
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0400); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0600) })
	name := "Read-only deployment"
	_, err = m.Change(ctx, "admin", config.Change{PluginID: change.PluginID, InstanceID: localInstanceID, ExpectedRevision: store.Snapshot().Revision, IdempotencyKey: "database-readonly-yaml", Settings: &config.InstanceSettings{Name: &name}})
	if err != nil {
		t.Fatalf("read-only YAML blocked database settings: %v", err)
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	roots = []config.RootSettings{{ID: "outside", Name: "Outside", Path: &outside}}
	_, err = m.Change(ctx, "admin", config.Change{PluginID: change.PluginID, InstanceID: localInstanceID, ExpectedRevision: store.Snapshot().Revision, IdempotencyKey: "database-deny-outside", Settings: &config.InstanceSettings{MusicRoots: &roots}})
	code(t, err, "ACCESS_DENIED")
	// Empty means intentionally unconfigured, and must not revive YAML defaults.
	roots = []config.RootSettings{}
	_, err = m.Change(ctx, "admin", config.Change{PluginID: change.PluginID, InstanceID: localInstanceID, ExpectedRevision: store.Snapshot().Revision, IdempotencyKey: "database-remove-all", Settings: &config.InstanceSettings{MusicRoots: &roots}})
	if err != nil {
		t.Fatal(err)
	}
	loaded, err = config.Load(path, nil)
	if err != nil || !loaded.Plugins[0].Instances[0].PendingConfiguration || len(loaded.Plugins[0].Instances[0].Config.MusicRoots) != 0 {
		t.Fatalf("empty settings not preserved: %v", err)
	}
	// Revoke deployment permissions while keeping database contents intact.
	if err := os.WriteFile(path, []byte(strings.Replace(raw, strconv.Quote(music), strconv.Quote(outside), 1)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Load(path, nil); err != nil {
		t.Fatal(err)
	}
}
