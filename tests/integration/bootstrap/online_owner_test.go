package bootstrap_test

import (
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/app/bootstrap"
	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/plugins/onlinehost"
)

func TestConfiguredOnlineOwnersStaySeparate(t *testing.T) {
	const firstID = "3bdd4699-d687-43f4-a96f-1ff50d5b2cce"
	const secondID = "ef74c8ed-9cce-491d-9075-9a80e180884d"
	cfg := serverConfig(t)
	cfg.Plugins = []config.Plugin{{PluginID: "musicfs.plugin.source.netease", Instances: []config.Instance{{InstanceID: firstID}, {InstanceID: secondID, OwnerUserID: "other-user"}}}}
	cookies := withSessions(t, &cfg)
	base, stop := serve(t, cfg, bootstrap.ServeOptions{})
	assertInstances := func(owner string, ids []string) {
		t.Helper()
		var list struct{ Items []onlinehost.Instance }
		if status := readJSON(t, base, "/api/v1/online-instances", cookies[owner], &list); status != 200 || len(list.Items) != len(ids) {
			t.Fatalf("online instance list crossed owner boundary: status %d, items %+v", status, list.Items)
		}
		for i, id := range ids {
			if list.Items[i].InstanceID != id {
				t.Fatal("configured owner was ignored", list.Items)
			}
		}
	}
	assertInstances("admin", []string{firstID})
	assertInstances("other-user", []string{secondID})
	stop()
	// Reopening with a changed owner must not retain the previous user's binding.
	cfg.Plugins[0].Instances[0].OwnerUserID = "other-user"
	base, stop = serve(t, cfg, bootstrap.ServeOptions{})
	defer stop()
	assertInstances("admin", nil)
	assertInstances("other-user", []string{firstID, secondID})
}
