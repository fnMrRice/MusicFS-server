package config_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/library/musicdirs"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugindb"
)

func defaultDirectoriesDocument(t *testing.T, permissions string) (*config.Document, map[string]string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	path := filepath.Join(t.TempDir(), "server.yaml")
	raw := `configVersion: 1
pluginBundle: ''
allowLoopbackHttp: true
allowedOrigins: ['http://127.0.0.1:8080']
plugins:
  - pluginId: musicfs.plugin.source.localfiles
    executable: './local-worker'
    enabled: true
    instances:
      - instanceId: local
        enabled: true
        config: {}
` + permissions
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	overrides := map[string]string{"ADMIN_TOKEN": "test-credential-at-least-24-characters"}
	doc, err := config.ReadDocument(path, overrides)
	if err != nil {
		t.Fatal(err)
	}
	return doc, overrides
}

func TestDefaultMusicPermissionDoesNotCreateStateOrSelectRoots(t *testing.T) {
	doc, _ := defaultDirectoriesDocument(t, "")
	i := doc.Config.Plugins[0].Instances[0]
	if got := config.AllowedDirectories(i); !reflect.DeepEqual(got, []string{musicdirs.Default()}) {
		t.Fatalf("unexpected default permissions: %v", got)
	}
	if len(i.Config.MusicRoots) != 0 || !i.PendingConfiguration {
		t.Fatal("default permissions selected music roots")
	}
	if _, err := os.Stat(config.PluginDatabasePath(doc.Config)); !os.IsNotExist(err) {
		t.Fatalf("configuration read created persistent state: %v", err)
	}
	if _, err := os.Stat(musicdirs.Default()); !os.IsNotExist(err) {
		t.Fatalf("configuration read created a music directory: %v", err)
	}
}

func TestDirectoryDiscoveryPersistsOnceIncludingEmptyResults(t *testing.T) {
	for _, empty := range []bool{false, true} {
		t.Run(map[bool]string{false: "found", true: "empty"}[empty], func(t *testing.T) {
			doc, overrides := defaultDirectoriesDocument(t, "")
			music := filepath.Join(t.TempDir(), "NeteaseMusic")
			calls := 0
			c, err := config.InitializeLocalDirectories(context.Background(), doc.Config, func() []string {
				calls++
				if empty {
					return nil
				}
				return []string{music, music, doc.Config.DataRoot, filepath.Join(doc.Config.CacheRoot, "Music")}
			})
			if err != nil {
				t.Fatal(err)
			}
			want := []string{musicdirs.Default()}
			if !empty {
				want = append(want, music)
			}
			if got := config.AllowedDirectories(c.Plugins[0].Instances[0]); !reflect.DeepEqual(got, want) {
				t.Fatalf("unexpected initialized permissions: %v", got)
			}
			restarted, err := config.ReadDocument(doc.Path, overrides)
			if err != nil {
				t.Fatal(err)
			}
			if got := config.AllowedDirectories(restarted.Config.Plugins[0].Instances[0]); !reflect.DeepEqual(got, want) {
				t.Fatalf("restart lost permissions: %v", got)
			}
			if restarted.Revision == doc.Revision {
				t.Fatal("discovery state did not participate in configuration revision")
			}
			_, err = config.InitializeLocalDirectories(context.Background(), restarted.Config, func() []string {
				calls++
				return []string{filepath.Join(t.TempDir(), "new-Music")}
			})
			if err != nil || calls != 1 {
				t.Fatalf("discovery repeated: calls=%d err=%v", calls, err)
			}
			again, err := config.ReadDocument(doc.Path, overrides)
			if err != nil || again.Revision != restarted.Revision {
				t.Fatalf("restart changed the saved revision: %v", err)
			}
			raw, err := os.ReadFile(doc.Path)
			if err != nil || string(raw) != string(doc.Raw) {
				t.Fatalf("discovery rewrote deployment YAML: %v", err)
			}
		})
	}
}

func TestExplicitDirectoryPermissionsOverrideDefaultsAndDisableDiscovery(t *testing.T) {
	for _, permissions := range []string{"[]", "['./manual']"} {
		t.Run(permissions, func(t *testing.T) {
			doc, _ := defaultDirectoriesDocument(t, "        permissions:\n          readDirectories: "+permissions+"\n")
			before := config.AllowedDirectories(doc.Config.Plugins[0].Instances[0])
			c, err := config.InitializeLocalDirectories(context.Background(), doc.Config, func() []string {
				t.Fatal("explicit permissions triggered discovery")
				return nil
			})
			if err != nil || !reflect.DeepEqual(config.AllowedDirectories(c.Plugins[0].Instances[0]), before) {
				t.Fatalf("explicit permissions changed: %v", err)
			}
			if _, err := os.Stat(config.PluginDatabasePath(c)); !os.IsNotExist(err) {
				t.Fatalf("explicit permissions created discovery state: %v", err)
			}
		})
	}
}

func TestSavedDefaultPermissionsDoNotOverrideRevocationOrEmptySettings(t *testing.T) {
	doc, overrides := defaultDirectoriesDocument(t, "")
	music := filepath.Join(t.TempDir(), "Music")
	if _, err := config.InitializeLocalDirectories(context.Background(), doc.Config, func() []string { return []string{music} }); err != nil {
		t.Fatal(err)
	}
	db, err := plugindb.Open(context.Background(), config.PluginDatabasePath(doc.Config), "musicfs.plugin.source.localfiles", "local")
	if err != nil {
		t.Fatal(err)
	}
	value, _ := json.Marshal(config.LocalSettings{Name: "Saved empty selection", MusicRoots: []config.MusicRoot{}})
	_, err = db.CompareAndSet(context.Background(), pluginproto.StateRequest{Key: plugindb.ConfigurationKey, Value: value})
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	restarted, err := config.ReadDocument(doc.Path, overrides)
	if err != nil {
		t.Fatal(err)
	}
	i := restarted.Config.Plugins[0].Instances[0]
	if len(i.Config.MusicRoots) != 0 || !i.PendingConfiguration || i.Name != "Saved empty selection" {
		t.Fatal("default discovery replaced saved settings")
	}
	revoked := append(append([]byte{}, doc.Raw...), []byte("        permissions:\n          readDirectories: []\n")...)
	if err := os.WriteFile(doc.Path, revoked, 0600); err != nil {
		t.Fatal(err)
	}
	restarted, err = config.ReadDocument(doc.Path, overrides)
	if err != nil || len(config.AllowedDirectories(restarted.Config.Plugins[0].Instances[0])) != 0 {
		t.Fatalf("saved discovery bypassed revocation: %v", err)
	}
}

func TestLegacyRootsRetainAuthorityAlongsideMusicDefault(t *testing.T) {
	doc, overrides := defaultDirectoriesDocument(t, "")
	raw := strings.Replace(string(doc.Raw), "        config: {}", "        config:\n          musicRoots:\n            - id: old\n              name: Existing\n              path: './legacy-music'", 1)
	parsed, err := config.ParseDocument([]byte(raw), doc.Path, overrides)
	if err != nil {
		t.Fatal(err)
	}
	i := parsed.Config.Plugins[0].Instances[0]
	want := []string{filepath.Join(filepath.Dir(doc.Path), "legacy-music"), musicdirs.Default()}
	if !reflect.DeepEqual(config.AllowedDirectories(i), want) || len(i.Config.MusicRoots) != 1 {
		t.Fatal("legacy root authority or selection changed")
	}
}

func TestDisabledLocalInstancesDoNotDiscoverDirectories(t *testing.T) {
	for _, disablePlugin := range []bool{false, true} {
		t.Run(map[bool]string{false: "instance", true: "plugin"}[disablePlugin], func(t *testing.T) {
			doc, _ := defaultDirectoriesDocument(t, "")
			if disablePlugin {
				doc.Config.Plugins[0].Enabled = false
			} else {
				doc.Config.Plugins[0].Instances[0].Enabled = false
			}
			_, err := config.InitializeLocalDirectories(context.Background(), doc.Config, func() []string {
				t.Fatal("disabled instance triggered discovery")
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(config.PluginDatabasePath(doc.Config)); !os.IsNotExist(err) {
				t.Fatalf("disabled instance created discovery state: %v", err)
			}
		})
	}
}
