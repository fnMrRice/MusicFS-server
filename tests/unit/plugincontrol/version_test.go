package plugincontrol_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/bundledplugin"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginconfig"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugincontrol"
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
	var item map[string]any
	if err != nil || json.Unmarshal(raw, &item) != nil || item["version"] != bundledplugin.NetEaseVersion {
		t.Fatal("unstarted plugin must include its declared version", string(raw), err)
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
	for _, invalid := range []string{"", " ", "v1.0.0", "1.0", "1.0.0-01"} {
		version = invalid
		if got := manager.Snapshot().Items[0].Version; got != bundledplugin.NetEaseVersion {
			t.Fatalf("invalid runtime version replaced package declaration: %q", got)
		}
	}
}

func TestPluginSnapshotRequiresDeclaredVersionWithoutInstances(t *testing.T) {
	for _, id := range []string{"musicfs.plugin.source.localfiles", "musicfs.plugin.decoder.ffmpeg", "musicfs.plugin.decoder.ncm", "musicfs.scraper.local", "musicfs.plugin.source.netease", "musicfs.plugin.source.qqmusic", "musicfs.plugin.source.applemusic"} {
		manager, err := plugincontrol.New(t.Context(), config.Config{Plugins: []config.Plugin{{PluginID: id}}}, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if got := manager.Snapshot().Items[0].Version; !pluginproto.ValidPluginVersion(got) || got != bundledplugin.Version(id) {
			t.Fatalf("plugin %s has no declared release: %q", id, got)
		}
		manager.Close()
	}
	if manager, err := plugincontrol.New(t.Context(), config.Config{Plugins: []config.Plugin{{PluginID: "third.party.unknown"}}}, nil, nil); err == nil {
		manager.Close()
		t.Fatal("unknown plugin was assigned an invented release")
	}
}
