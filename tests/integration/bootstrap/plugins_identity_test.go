package bootstrap_test

import (
	"path/filepath"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/instanceid"
	"github.com/fnMrRice/MusicFS-server/src/bootstrap"
	"github.com/fnMrRice/MusicFS-server/src/config"
	"github.com/fnMrRice/MusicFS-server/src/library"
)

func TestConfiguredLocalProviderHasPluginIdentityWithoutStartingWorker(t *testing.T) {
	for _, scenario := range []struct {
		name                      string
		plugin, instance, pending bool
	}{{"pending", true, true, true}, {"plugin-disabled", false, true, false}, {"instance-disabled", true, false, false}} {
		t.Run(scenario.name, func(t *testing.T) {
			cfg := serverConfig(t)
			cfg.Plugins = []config.Plugin{{PluginID: "musicfs.plugin.source.localfiles", Enabled: scenario.plugin, Executable: filepath.Join(t.TempDir(), "missing-worker"), Instances: []config.Instance{{InstanceID: instanceid.Canonical("renamed-local-instance"), Name: "Administrator alias", Enabled: scenario.instance, PendingConfiguration: scenario.pending, Permissions: &config.Permissions{ReadDirectories: []string{}}}}}}
			cookies := withSessions(t, &cfg)
			for attempt := range 2 {
				base, stop := serve(t, cfg, bootstrap.ServeOptions{})
				var response struct{ Items []library.Provider }
				status := readJSON(t, base, "/api/v1/provider-instances", cookies["admin"], &response)
				stop()
				if status != 200 || len(response.Items) != 1 || response.Items[0].Plugin == nil {
					t.Fatal("inactive local provider lost plugin declaration", attempt, status, response)
				}
				p := response.Items[0]
				if p.Plugin.ID != "musicfs.plugin.source.localfiles" || p.Plugin.Name != "Local Music" || p.Plugin.LocalizedNames["zh-Hans"] != "本地音乐" || p.Availability != "offline" || len(p.Capabilities) != 0 {
					t.Fatalf("invalid inactive provider: %+v", p)
				}
				if p.Name != "Administrator alias" {
					t.Fatal("plugin display name replaced configured alias")
				}
			}
		})
	}
}
