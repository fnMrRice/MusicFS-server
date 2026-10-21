package config_test

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestExplicitStandaloneOnlineSelectionSurvivesFullBundleDiscovery(t *testing.T) {
	for _, platform := range []string{"netease", "qqmusic", "applemusic"} {
		t.Run(platform, func(t *testing.T) {
			digest := strings.Repeat("c", 64)
			doc := bundledDocument(t, `plugins:
  - pluginId: musicfs.plugin.source.`+platform+`
    execution: process
    executable: './musicfs-plugin-`+platform+`'
    sha256: '`+digest+`'
    args: []
    instances:
      - instanceId: explicit-account
        enabled: false
        config: {}
`)
			plugin := discoveredPlugin(t, doc, "musicfs.plugin.source."+platform)
			if filepath.Base(plugin.Executable) != "musicfs-plugin-"+platform ||
				len(plugin.Args) != 0 || plugin.SHA256 != digest || len(plugin.Instances) != 1 ||
				plugin.Instances[0].Enabled || len(doc.Config.Plugins) != 7 {
				t.Fatalf("full bundle discovery replaced an explicit standalone choice: %+v", plugin)
			}
		})
	}
}

func TestPinnedFullBundleRetainsIdentityAndCanStartOnline(t *testing.T) {
	digest := strings.Repeat("d", 64)
	doc := bundledDocument(t, `plugins:
  - pluginId: musicfs.plugin.source.netease
    execution: process
    enabled: true
    executable: './musicfs-plugins'
    sha256: '`+digest+`'
    args: ['--plugin=netease']
`)
	plugin := discoveredPlugin(t, doc, "musicfs.plugin.source.netease")
	if filepath.Base(plugin.Executable) != "musicfs-plugins" ||
		plugin.SHA256 != digest || len(plugin.Args) != 1 || plugin.Args[0] != "--plugin=netease" ||
		!plugin.CanStart(plugin.Instances[0]) {
		t.Fatalf("discovery changed a selected bundle or treated its online role as unconfigured: %+v", plugin)
	}
}

func TestFullBundleDoesNotDiscoverDatabaseOrDSPAsOrdinaryPlugins(t *testing.T) {
	doc := discoveredDocument(t, "", []string{"musicfs-plugins", "musicfs-plugin-storage-sqlite", "musicfs-plugin-dsp-gain"})
	if len(doc.Config.Plugins) != 7 {
		t.Fatalf("ordinary discovery must contain four local and three online identities: %+v", doc.Config.Plugins)
	}
	for _, plugin := range doc.Config.Plugins {
		if strings.Contains(plugin.PluginID, "sqlite") || strings.Contains(plugin.PluginID, "dsp") {
			t.Fatalf("dedicated database/DSP protocol was added to ordinary discovery: %+v", plugin)
		}
	}
}
