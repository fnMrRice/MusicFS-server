package config_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/instanceid"
	"github.com/fnMrRice/MusicFS-server/src/core/config"
)

func discoveredPlugin(t *testing.T, doc *config.Document, id string) config.Plugin {
	t.Helper()
	for _, plugin := range doc.Config.Plugins {
		if plugin.PluginID == id {
			return plugin
		}
	}
	t.Fatalf("plugin was not found: %s", id)
	return config.Plugin{}
}

func TestBundledOnlineSelectionPreservesUserSettings(t *testing.T) {
	doc := bundledDocument(t, `plugins:
  - pluginId: musicfs.plugin.source.netease
    execution: process
    executable: './musicfs-plugins'
    args: ['--plugin', 'netease']
    enabled: false
    instances:
      - instanceId: custom-online
        name: Personal catalogue
        enabled: false
        config: {}
  - pluginId: musicfs.plugin.source.localfiles
    enabled: false
    instances:
      - instanceId: custom-local
        enabled: false
        permissions: {readDirectories: ['./allowed']}
        config: {}
`)
	online := discoveredPlugin(t, doc, "musicfs.plugin.source.netease")
	if filepath.Base(online.Executable) != "musicfs-plugins" || len(online.Args) != 2 || online.Args[1] != "netease" {
		t.Fatalf("explicit full bundle dispatch was replaced: %+v", online)
	}
	instance := online.Instances[0]
	if online.Enabled || instance.Enabled || instance.InstanceID != instanceid.Canonical("custom-online") ||
		instance.Name != "Personal catalogue" || len(online.Instances) != 1 {
		t.Fatalf("migration changed user settings: %+v", online)
	}
	local := discoveredPlugin(t, doc, "musicfs.plugin.source.localfiles")
	if local.Enabled || local.Instances[0].Enabled || local.Instances[0].Permissions == nil ||
		len(local.Instances[0].Permissions.ReadDirectories) != 1 ||
		filepath.Base(local.Instances[0].Permissions.ReadDirectories[0]) != "allowed" {
		t.Fatalf("migration changed local permissions: %+v", local)
	}
}

func TestBuiltinDiscoveryUsesFixedCatalogWithoutPrograms(t *testing.T) {
	doc := discoveredDocument(t, "", []string{"musicfs-plugin-qqmusic", "musicfs-plugin-unknown"})
	if len(doc.Config.Plugins) != 8 {
		t.Fatalf("builtin catalog changed with available programs: %+v", doc.Config.Plugins)
	}
	for _, plugin := range doc.Config.Plugins {
		if !plugin.InProcess() || plugin.Executable != "" || strings.Contains(plugin.PluginID, "unknown") {
			t.Fatal("filesystem programs affected builtin discovery")
		}
	}
	doc = discoveredDocument(t, "", []string{"musicfs-plugins"})
	if len(doc.Config.Plugins) != 8 {
		t.Fatal("full bundle did not supply all ordinary plugin identities")
	}
}

func TestMissingOrPinnedOnlineSelectionRemainsPending(t *testing.T) {
	for _, test := range []struct {
		name, executable, extra string
		files                   []string
	}{
		{"missing-full-bundle", "musicfs-plugins", "    args: ['--plugin', 'netease']\n", []string{"musicfs-plugin-netease", "musicfs-plugin-online"}},
		{"missing-standalone", "musicfs-plugin-netease", "", nil},
		{"pinned-old-binary", "musicfs-plugin-netease", "    sha256: '" + strings.Repeat("a", 64) + "'\n", []string{"musicfs-plugins"}},
		{"custom-bundle-arguments", "musicfs-plugins", "    args: ['--plugin', 'netease', '--custom']\n", []string{"musicfs-plugins", "musicfs-plugin-netease"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			doc := discoveredDocument(t, "plugins:\n  - pluginId: musicfs.plugin.source.netease\n    execution: process\n    executable: './"+test.executable+"'\n"+test.extra+`    enabled: true
    instances:
      - instanceId: a5e47aac-f949-5e45-9709-f28a0d8443df
        enabled: true
        config: {}
`, test.files)
			plugin := discoveredPlugin(t, doc, "musicfs.plugin.source.netease")
			if filepath.Base(plugin.Executable) != test.executable || !plugin.Pending(plugin.Instances[0]) || plugin.CanStart(plugin.Instances[0]) {
				t.Fatalf("unsafe or unavailable migration was marked ready: %+v", plugin)
			}
			if test.name == "pinned-old-binary" && plugin.SHA256 != strings.Repeat("a", 64) {
				t.Fatal("migration removed an executable digest")
			}
		})
	}
}

func TestOnlineDiscoveryPreservesExplicitExternalConfiguration(t *testing.T) {
	doc := bundledDocument(t, `plugins:
  - pluginId: musicfs.plugin.source.qqmusic
    execution: process
    executable: './custom/qq-worker'
    args: ['--custom', 'literal argument']
    enabled: true
    instances:
      - instanceId: fc2264bd-13bc-5dc6-be83-6c4428cf4d40
        enabled: false
        name: Custom QQ
        config: {}
`)
	plugin := discoveredPlugin(t, doc, "musicfs.plugin.source.qqmusic")
	if filepath.Base(plugin.Executable) != "qq-worker" || len(plugin.Args) != 2 || plugin.Args[1] != "literal argument" ||
		!plugin.Enabled || plugin.Instances[0].Enabled || plugin.Instances[0].Name != "Custom QQ" {
		t.Fatalf("external deployment was changed: %+v", plugin)
	}
}
