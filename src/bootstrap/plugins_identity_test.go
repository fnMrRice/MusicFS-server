package bootstrap

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/config"
	"github.com/fnMrRice/MusicFS-server/src/jobs"
	"github.com/fnMrRice/MusicFS-server/src/library"
)

type identityRepository struct{ library.Repository }

func (identityRepository) ProviderScanState(context.Context, string, string) string { return "idle" }

func TestConfiguredLocalProviderHasPluginIdentityWithoutStartingWorker(t *testing.T) {
	for _, scenario := range []struct {
		name                      string
		plugin, instance, pending bool
	}{
		{name: "pending", plugin: true, instance: true, pending: true},
		{name: "plugin-disabled", instance: true},
		{name: "instance-disabled", plugin: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			cfg := config.Config{DataRoot: t.TempDir(), Plugins: []config.Plugin{{
				PluginID: "musicfs.localfiles", Enabled: scenario.plugin,
				// Disabled and unconfigured providers must not require an executable.
				Executable: "missing-worker", Instances: []config.Instance{{
					InstanceID: "renamed-local-instance", Name: "Administrator alias",
					Enabled: scenario.instance, PendingConfiguration: scenario.pending,
				}},
			}}}
			for attempt := range 2 {
				runtimes, _, _, _ := configurePlugins(context.Background(), cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
				if len(runtimes) != 1 || runtimes[0].Worker != nil {
					t.Fatal("inactive local provider started a worker", attempt)
				}
				service := library.New(identityRepository{}, jobs.New(nil), runtimes, nil)
				providers := service.Providers(context.Background(), "admin")
				if len(providers) != 1 || providers[0].Plugin == nil {
					t.Fatal("inactive local provider lost its plugin declaration", attempt)
				}
				provider := providers[0]
				if provider.Plugin.ID != "musicfs.localfiles" || provider.Plugin.Name != "Local Music" || provider.Plugin.LocalizedNames["zh-Hans"] != "本地音乐" || provider.Availability != "offline" || len(provider.Capabilities) != 0 {
					t.Fatalf("invalid inactive provider: %+v", provider)
				}
				if provider.Name != "Administrator alias" {
					t.Fatal("plugin display name replaced the configured instance alias")
				}
			}
		})
	}
}
