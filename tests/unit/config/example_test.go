package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestOfficialExampleEnablesBuiltinPluginsAndPreservesDisable(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(testkit.RepositoryRoot(), "configs", "server.example.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	source := strings.ReplaceAll(string(raw), "\r\n", "\n")
	for _, tc := range []struct {
		name, setting                  string
		pluginEnabled, instanceEnabled bool
	}{
		{"default", "", true, true},
		{"plugin disabled", "    enabled: true\n", false, true},
		{"instance disabled", "        enabled: true\n", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := source
			if tc.setting != "" {
				input = strings.Replace(input, tc.setting, strings.Replace(tc.setting, "true", "false", 1), 1)
				if input == source {
					t.Fatal("official example does not declare the enabled setting")
				}
			}
			// 使用全新安装路径，避免仓库运行数据影响示例的初始状态。
			path := filepath.Join(t.TempDir(), "configs", "server.yaml")
			document, err := config.ParseDocument([]byte(input), path, map[string]string{"ADMIN_TOKEN": "official-example-test-credential-only"})
			if err != nil {
				t.Fatal(err)
			}
			if len(document.Config.Plugins) != 7 {
				t.Fatal("official example changed the bundled plugin selection")
			}
			for _, plugin := range document.Config.Plugins {
				if !plugin.InProcess() || len(plugin.Instances) != 1 {
					t.Fatalf("official example must use one builtin instance per plugin: %s", plugin.PluginID)
				}
				instance := plugin.Instances[0]
				wantPlugin, wantInstance := true, true
				if plugin.PluginID == "musicfs.plugin.source.localfiles" {
					wantPlugin, wantInstance = tc.pluginEnabled, tc.instanceEnabled
				}
				if plugin.Enabled != wantPlugin || instance.Enabled != wantInstance {
					t.Fatalf("unexpected enabled state for %s: plugin=%t instance=%t", plugin.PluginID, plugin.Enabled, instance.Enabled)
				}
				needsConfiguration := plugin.PluginID == "musicfs.plugin.source.localfiles" || plugin.PluginID == "musicfs.plugin.decoder.ffmpeg"
				if instance.PendingConfiguration != needsConfiguration || plugin.Pending(instance) != (wantPlugin && wantInstance && needsConfiguration) || plugin.CanStart(instance) != (wantPlugin && wantInstance && !needsConfiguration) {
					t.Fatalf("unexpected startup readiness for %s", plugin.PluginID)
				}
			}
		})
	}
}
