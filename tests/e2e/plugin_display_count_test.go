package e2e_test

import (
	"encoding/json"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/config"
	"github.com/fnMrRice/MusicFS-server/src/plugincontrol"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func pluginDisplaySnapshot(t *testing.T, c *client) map[string]plugincontrol.Plugin {
	t.Helper()
	code, raw, _ := c.request(t, "GET", "/api/v1/admin/plugins", nil, "")
	var snapshot plugincontrol.Snapshot
	if code != 200 || json.Unmarshal(raw, &snapshot) != nil {
		t.Fatalf("plugin display snapshot failed: %d %s", code, raw)
	}
	testkit.ValidateHTTP(t, "PluginManagementSnapshot", raw)
	items := map[string]plugincontrol.Plugin{}
	for _, item := range snapshot.Items {
		items[item.PluginID] = item
	}
	return items
}

func TestPluginDisplayCountsFollowDirectoriesAndAccountLifecycle(t *testing.T) {
	const localID = "b1548e96-3081-5c9c-8376-bf290e369d51"
	const onlineID = "a0462a4b-8754-5747-a8cf-29cf4d0a874f"
	const localPlugin = "musicfs.plugin.source.localfiles"
	const onlinePluginID = "musicfs.plugin.source.netease"
	roots := []config.MusicRoot{{ID: "one", Name: "First", Path: t.TempDir()}, {ID: "two", Name: "Second", Path: t.TempDir()}}
	cfg, base := yamlServiceConfig(t)
	cfg.Plugins = []config.Plugin{
		{PluginID: localPlugin, Executable: build(t, "musicfs-plugin-local"), Enabled: true,
			Instances: []config.Instance{{InstanceID: localID, Enabled: true, Config: config.InstanceConfig{MusicRoots: roots}}}},
		onlinePlugin("netease", managedOnlineFixture(t), onlineID),
	}
	path := writeYAMLConfiguration(t, cfg)
	stop := startAudioYAMLServer(t, path, cfg, base)
	defer stop()
	c := login(t, base, cfg.AdminToken)
	assertCount := func(id string, value int64, unit string) {
		t.Helper()
		item := pluginDisplaySnapshot(t, c)[id]
		if item.DisplayCount == nil || item.DisplayCount.Value != value || item.DisplayCount.LocalizedUnits["zh-Hans"] != unit {
			t.Fatalf("plugin display count did not follow plugin data: %s %+v", id, item.DisplayCount)
		}
		if len(item.Instances) != 1 {
			t.Fatalf("display count changed configured instances: %+v", item.Instances)
		}
	}
	assertCount(localPlugin, 2, "个来源")
	assertCount(onlinePluginID, 0, "个用户")
	authenticateManagedOnline(t, c, onlineID)
	assertCount(onlinePluginID, 1, "个用户")
	code, raw, _ := c.request(t, "POST", "/api/v1/online-instances/"+onlineID+"/account", map[string]string{"action": "logout"}, "display-count-logout")
	if code != 200 {
		t.Fatalf("plugin account logout failed: %d %s", code, raw)
	}
	assertCount(onlinePluginID, 0, "个用户")
	op := libraryManagementChange(t, c, "/api/v1/admin/plugin-instances/"+localID+"/configuration", map[string]any{"musicRoots": roots[:1]})
	waitLibraryOperation(t, c, op.ID)
	assertCount(localPlugin, 1, "个来源")
	op = libraryManagementChange(t, c, "/api/v1/admin/plugin-instances/"+localID+"/configuration", map[string]any{"musicRoots": []config.MusicRoot{}})
	waitLibraryOperation(t, c, op.ID)
	item := pluginDisplaySnapshot(t, c)[localPlugin]
	if item.DisplayCount != nil || len(item.Instances) != 1 || item.Instances[0].RuntimeState != "pending_configuration" {
		t.Fatalf("stopped plugin retained or synthesized a count: %+v", item)
	}
}
