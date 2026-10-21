package plugincontrol_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/config"
	"github.com/fnMrRice/MusicFS-server/src/pluginconfig"
	"github.com/fnMrRice/MusicFS-server/src/plugincontrol"
)

func TestPluginSnapshotUsesVerifiedInstanceVersion(t *testing.T) {
	const pluginID = "musicfs.plugin.source.netease"
	const first = "3bdd4699-d687-43f4-a96f-1ff50d5b2cce"
	const second = "3bdd4699-d687-43f4-a96f-1ff50d5b2ccf"
	manager, err := plugincontrol.New(context.Background(), config.Config{Plugins: []config.Plugin{
		{PluginID: pluginID, Instances: []config.Instance{{InstanceID: first}, {InstanceID: second}}},
	}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	raw, err := json.Marshal(manager.Snapshot().Items[0])
	if err != nil || strings.Contains(string(raw), `"version"`) {
		t.Fatal("unknown plugin version must be omitted", string(raw), err)
	}
	if err := manager.Register(first, plugincontrol.Hooks{
		PluginIdentity: func() *pluginproto.PluginIdentity { return pluginconfig.Identity(pluginID) },
	}, false); err != nil {
		t.Fatal(err)
	}
	version := "2.3.4-rc.1+fixture"
	if err := manager.Register(second, plugincontrol.Hooks{
		PluginVersion: func() string { return version },
	}, false); err != nil {
		t.Fatal(err)
	}
	if got := manager.Snapshot().Items[0].Version; got != version {
		t.Fatal("plugin version did not use the verified instance", got)
	}
	version = "3.0.0"
	if got := manager.SnapshotFor(context.Background(), "owner").Items[0].Version; got != version {
		t.Fatal("plugin version did not follow the current worker", got)
	}
}
