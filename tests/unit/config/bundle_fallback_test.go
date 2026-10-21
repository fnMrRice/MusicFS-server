package config_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/core/config"
)

func TestUnifiedOnlineDiscoveryAndStandalonePriority(t *testing.T) {
	for _, independentQQ := range []bool{false, true} {
		files := []string{"musicfs-plugin-online"}
		if independentQQ {
			files = append(files, "musicfs-plugin-qqmusic")
		}
		raw := "plugins:\n"
		for _, platform := range []string{"netease", "qqmusic", "applemusic"} {
			raw += "  - pluginId: musicfs.plugin.source." + platform + "\n    execution: process\n    enabled: true\n"
		}
		doc := discoveredDocument(t, raw, files)
		if len(doc.Config.Plugins) != 7 {
			t.Fatal("alternative entry points created duplicate or missing providers")
		}
		for _, plugin := range doc.Config.Plugins {
			platform := config.OnlinePlatform(plugin.PluginID)
			if platform == "" {
				continue
			}
			if !plugin.CanStart(plugin.Instances[0]) {
				t.Fatalf("available provider was not ready: %+v", plugin)
			}
			if independentQQ && platform == "qqmusic" {
				if filepath.Base(plugin.Executable) != "musicfs-plugin-qqmusic" || len(plugin.Args) != 0 {
					t.Fatal("standalone executable did not take discovery priority")
				}
			} else if filepath.Base(plugin.Executable) != "musicfs-plugin-online" ||
				len(plugin.Args) != 2 || plugin.Args[0] != "--plugin" || plugin.Args[1] != platform {
				t.Fatalf("unified executable has the wrong platform selector: %+v", plugin)
			}
		}
	}
}

func TestOnlineFallbackMigratesKnownUnpinnedStandalone(t *testing.T) {
	for _, target := range []string{"musicfs-plugins", "musicfs-plugin-online"} {
		doc := discoveredDocument(t, `plugins:
  - pluginId: musicfs.plugin.source.netease
    execution: process
    executable: './musicfs-plugin-netease'
    enabled: false
    instances:
      - instanceId: a5e47aac-f949-5e45-9709-f28a0d8443df
        enabled: false
        name: Preserved
        config: {}
`, []string{target})
		plugin := discoveredPlugin(t, doc, "musicfs.plugin.source.netease")
		if filepath.Base(plugin.Executable) != target || len(plugin.Args) != 2 || plugin.Args[1] != "netease" ||
			plugin.Enabled || plugin.Instances[0].Enabled || plugin.Instances[0].Name != "Preserved" {
			t.Fatalf("fallback migration lost configuration: %+v", plugin)
		}
	}
}

func TestUnifiedOnlineFallbackPreservesPinsAndCustomArguments(t *testing.T) {
	for _, target := range []string{"musicfs-plugins", "musicfs-plugin-online"} {
		for _, extra := range []string{
			"    sha256: '" + strings.Repeat("b", 64) + "'\n",
			"    args: ['--custom']\n",
		} {
			doc := discoveredDocument(t, "plugins:\n  - pluginId: musicfs.plugin.source.netease\n    execution: process\n    executable: './musicfs-plugin-netease'\n"+extra+`    enabled: true
    instances:
      - instanceId: a5e47aac-f949-5e45-9709-f28a0d8443df
        enabled: true
        config: {}
`, []string{target})
			plugin := discoveredPlugin(t, doc, "musicfs.plugin.source.netease")
			if filepath.Base(plugin.Executable) != "musicfs-plugin-netease" || !plugin.Pending(plugin.Instances[0]) {
				t.Fatalf("fallback silently changed a pinned or custom deployment: %+v", plugin)
			}
			if strings.Contains(extra, "sha256") && plugin.SHA256 != strings.Repeat("b", 64) {
				t.Fatal("fallback removed the pinned executable digest")
			}
		}
	}
}

func TestExplicitUnifiedOnlineChoiceAndSelectorValidation(t *testing.T) {
	for _, executable := range []string{"musicfs-plugins", "musicfs-plugin-online"} {
		for _, selector := range []string{"netease", "qqmusic"} {
			for _, args := range []string{"'--plugin', '" + selector + "'", "'--plugin=" + selector + "'"} {
				doc := discoveredDocument(t, `plugins:
  - pluginId: musicfs.plugin.source.netease
    execution: process
    enabled: true
    executable: './`+executable+`'
    args: [`+args+`]
    instances:
      - instanceId: a5e47aac-f949-5e45-9709-f28a0d8443df
        enabled: true
        config: {}
`, []string{"musicfs-plugins", "musicfs-plugin-netease", "musicfs-plugin-online"})
				plugin := discoveredPlugin(t, doc, "musicfs.plugin.source.netease")
				if filepath.Base(plugin.Executable) != executable || !plugin.Enabled ||
					plugin.CanStart(plugin.Instances[0]) != (selector == "netease") {
					t.Fatalf("explicit unified choice or selector was mishandled: %+v", plugin)
				}
			}
		}
	}
}
