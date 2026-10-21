package plugins_test

import (
	"context"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginhost"
)

func TestOnlineWorkerCachesPluginIdentityAndRejectsUntrustedDescriptors(t *testing.T) {
	path := onlineBinary(t, "./tests/testkit/online-identity-worker", "online-identity")
	cfg := onlineConfig(t, path, online.Netease)
	w := onlineStart(t, cfg)
	if w.PluginVersion() != "0.1.0" {
		t.Fatal("verified worker version was not retained", w.PluginVersion())
	}
	d := w.Descriptor()
	if d.Plugin == nil || d.Plugin.ID != "musicfs.plugin.source.netease" || d.Plugin.Name != "Fixture Music" || d.Plugin.LocalizedNames["zh-Hans"] != "测试音乐" {
		t.Fatal("worker display metadata was replaced by host defaults", d)
	}
	d.Plugin.Name = "Changed copy"
	d.Plugin.LocalizedNames["zh-Hans"] = "Changed translation"
	d.Plugin.Features[0] = "changed-feature"
	d.Capabilities[0].Supported = !d.Capabilities[0].Supported
	d.ConfigSchema["properties"].(map[string]any)["injected"] = true
	if after := w.Descriptor(); after.Plugin.Name != "Fixture Music" || after.Plugin.LocalizedNames["zh-Hans"] != "测试音乐" || after.Validate(online.Netease) != nil {
		t.Fatal("descriptor copy mutated the startup cache", after)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if after := w.Descriptor(); after.Plugin.Name != "Fixture Music" {
		t.Fatal("offline worker lost its verified identity")
	}
	if w.PluginVersion() != "0.1.0" {
		t.Fatal("offline worker lost its verified version")
	}
	for _, mode := range []string{"bad-id", "bad-name", "bad-locale", "stale-revision", "missing-plugin", "missing-source-feature", "changed-display", "wrong-multiplicity", "bad-version", "bad-protocol", "capability-mismatch"} {
		t.Run(mode, func(t *testing.T) {
			cfg := onlineConfig(t, path, online.Netease)
			cfg.Args = []string{mode}
			invalid, err := pluginhost.StartOnline(context.Background(), cfg)
			if err == nil {
				_ = invalid.Close()
				t.Fatal("invalid worker descriptor accepted")
			}
			onlineExpect(t, err, "PROTOCOL_MISMATCH")
		})
	}
}

func TestIndependentOnlineReleaseAndCapabilitySubset(t *testing.T) {
	path := onlineBinary(t, "./tests/testkit/online-identity-worker", "online-compatibility")
	for _, mode := range []string{"new-release", "capability-subset"} {
		t.Run(mode, func(t *testing.T) {
			cfg := onlineConfig(t, path, online.Netease)
			cfg.Args = []string{mode}
			w := onlineStart(t, cfg)
			if mode == "new-release" && w.PluginVersion() != "2.3.4-rc.1+fixture" {
				t.Fatal("host replaced the independent plugin version", w.PluginVersion())
			}
			if mode == "capability-subset" {
				d := w.Descriptor()
				if len(d.Capabilities) != 1 || d.Capabilities[0].Name != "metadata.track" {
					t.Fatalf("host replaced plugin capabilities: %+v", d.Capabilities)
				}
				_, err := w.Search(context.Background(), online.SearchRequest{Query: "fixture", Limit: 1})
				onlineExpect(t, err, "UNSUPPORTED")
			}
		})
	}
}
