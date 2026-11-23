package e2e_test

import (
	"maps"
	"slices"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/plugins/settings"
	"github.com/google/uuid"
)

func TestOnlinePlaybackSettingsPersistAcrossRestart(t *testing.T) {
	const netease = "musicfs.plugin.source.netease"
	const bilibili = "musicfs.plugin.source.bilibili"
	cfg, base := yamlServiceConfig(t)
	for _, id := range []string{netease, bilibili} {
		cfg.Plugins = append(cfg.Plugins, config.Plugin{PluginID: id, Execution: "builtin", Enabled: true,
			Instances: []config.Instance{{InstanceID: uuid.NewString(), Enabled: true}}})
	}
	path := writeYAMLConfiguration(t, cfg)
	stop := startAudioYAMLServer(t, path, cfg, base)
	c := login(t, base, cfg.AdminToken)
	plugins := pluginDisplaySnapshot(t, c)
	saved := map[string]settings.Values{
		netease:  {"playbackQuality": "hires", "cacheLimitMiB": float64(32), "streamWhileDownloading": false},
		bilibili: {"cacheLimitMiB": float64(12), "streamWhileDownloading": true},
	}
	for _, id := range []string{netease, bilibili} {
		if !plugins[id].SettingsAvailable {
			t.Fatalf("missing settings button for %s", id)
		}
		page := readPluginSettings(t, c, id)
		defaults := settings.Values{"cacheLimitMiB": float64(1024), "streamWhileDownloading": true}
		if id == netease {
			defaults["playbackQuality"] = "exhigh"
			var levels []string
			for _, field := range page.Schema.Fields {
				if field.Key == "playbackQuality" && field.Type == "select" && field.DefaultValue == "exhigh" {
					for _, option := range field.Options {
						levels = append(levels, option.Value)
					}
				}
			}
			if !slices.Equal(levels, []string{"sky", "jymaster", "jyeffect", "hires", "lossless", "exhigh", "standard"}) {
				t.Fatalf("unexpected selectable playback qualities: %v", levels)
			}
		}
		if len(page.Schema.Fields) != len(defaults) || !maps.Equal(page.Values, defaults) {
			t.Fatalf("unexpected settings for %s: %+v", id, page)
		}
		request := settings.Request{ExpectedRevision: &page.Revision, SchemaRevision: page.Schema.Revision, Values: saved[id]}
		if code, raw, _ := c.request(t, "PUT", "/api/v1/plugin-settings/"+id, request, ""); code != 200 {
			t.Fatalf("saving %s settings failed: %d %s", id, code, raw)
		}
	}
	stop()
	startAudioYAMLServer(t, path, cfg, base)
	c = login(t, base, cfg.AdminToken)
	for id, want := range saved {
		if page := readPluginSettings(t, c, id); page.Revision != 1 || !maps.Equal(page.Values, want) {
			t.Fatalf("restart lost %s settings: %+v", id, page)
		}
	}
}
