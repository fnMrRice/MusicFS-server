package plugincontrol_test

import (
	"context"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/config"
	"github.com/fnMrRice/MusicFS-server/src/pluginconfig"
	"github.com/fnMrRice/MusicFS-server/src/plugincontrol"
	"github.com/fnMrRice/MusicFS-server/src/pluginproto"
)

func TestPluginSnapshotDeclaresIdentityAndInstanceSupport(t *testing.T) {
	const instanceID = "3bdd4699-d687-43f4-a96f-1ff50d5b2cce"
	const pluginID = "musicfs.plugin.source.netease"
	manager, err := plugincontrol.New(context.Background(), config.Config{Plugins: []config.Plugin{
		{PluginID: pluginID, Instances: []config.Instance{{InstanceID: instanceID, Name: "Old deployment note"}}},
		{PluginID: "musicfs.plugin.source.localfiles"},
	}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	snapshot := manager.Snapshot()
	if len(snapshot.Items) != 2 || snapshot.Items[0].Plugin == nil || !snapshot.Items[0].SupportsMultipleInstances || snapshot.Items[1].SupportsMultipleInstances {
		t.Fatal("missing plugin-owned identity or instance declaration", snapshot)
	}
	identity := pluginconfig.Identity(pluginID)
	identity.Name = "Verified worker alias"
	accountName := "Current account username"
	if err := manager.Register(instanceID, plugincontrol.Hooks{
		PluginIdentity: func() *pluginproto.PluginIdentity { return identity },
		InstanceName: func(ctx context.Context, owner string) string {
			if owner != "owner" {
				return ""
			}
			// Reentering the manager must not wait on a held snapshot lock.
			if manager.Gate(instanceID) == nil {
				t.Error("missing runtime")
			}
			return accountName
		},
	}, false); err != nil {
		t.Fatal(err)
	}
	snapshot = manager.Snapshot()
	if snapshot.Items[0].Plugin.Name != identity.Name {
		t.Fatal("verified worker identity was not preferred")
	}
	snapshot.Items[0].Plugin.Name = "API mutation"
	if manager.Snapshot().Items[0].Plugin.Name != identity.Name {
		t.Fatal("API snapshot aliases runtime identity")
	}
	if snapshot.Items[0].Instances[0].Name != identity.Name {
		t.Fatal("unscoped snapshot exposed a deployment note as an account name")
	}
	if got := manager.SnapshotFor(context.Background(), "owner").Items[0].Instances[0].Name; got != accountName {
		t.Fatal("scoped snapshot did not resolve the current username", got)
	}
	if got := manager.SnapshotFor(context.Background(), "other").Items[0].Instances[0].Name; got != identity.Name {
		t.Fatal("scoped snapshot leaked another owner's username", got)
	}
	accountName = ""
	if got := manager.SnapshotFor(context.Background(), "owner").Items[0].Instances[0].Name; got != identity.Name {
		t.Fatal("signed-out snapshot retained the account username", got)
	}
}
