package plugins_test

import (
	"context"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/online"
	"github.com/fnMrRice/MusicFS-server/src/pluginhost"
)

func TestOnlineWorkerCachesPluginIdentityAndRejectsUntrustedDescriptors(t *testing.T) {
	path := onlineBinary(t, "./tests/testkit/online-identity-worker", "online-identity")
	cfg := onlineConfig(t, path, online.Netease)
	w := onlineStart(t, cfg)
	d := w.Descriptor()
	if d.Plugin == nil || d.Plugin.ID != "musicfs.netease" || d.Plugin.Name != "Fixture Music" || d.Plugin.LocalizedNames["zh-Hans"] != "测试音乐" {
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
	for _, mode := range []string{"bad-id", "bad-name", "bad-locale", "stale-revision", "missing-plugin", "missing-source-feature"} {
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
