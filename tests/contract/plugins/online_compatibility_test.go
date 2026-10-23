package plugins_test

import (
	"encoding/json"
	"testing"

	onlineplugins "github.com/fnMrRice/MusicFS-server/plugins/source/online"
	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestOnlineReleaseVersionAndSubsetWireContract(t *testing.T) {
	d := onlineplugins.Describe(online.Netease)
	d.Capabilities = []online.Capability{{Name: "metadata.track", Supported: true, Reason: "Supported by this release"}}
	m := pluginproto.Manifest{
		PluginID: d.Plugin.ID, Plugin: d.Plugin, SupportsMultipleInstances: true,
		PluginVersion: "2.3.4-rc.1+fixture", ProtocolVersion: pluginproto.ProtocolVersion,
		Roles: []string{"provider"}, SourceType: pluginproto.SourceNetwork, Capabilities: []string{"metadata.track"},
		Systems: []string{"windows"}, Architectures: []string{"amd64"},
		MaxMessageBytes: pluginproto.MaxMessageBytes, MaxPageItems: online.MaxPageItems,
	}
	onlineContract(t, "Manifest", m)
	onlineContract(t, "Descriptor", d)
	if err := pluginproto.ValidateOnlineDescriptor(m, d, online.Netease); err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"", "01.2.3", "1.2", "1.2.3-01", "1.2.3\n"} {
		m.PluginVersion = version
		data, _ := json.Marshal(m)
		if onlineSchema(t, "Manifest").Validate(testkit.DecodeJSON(t, data)) == nil || pluginproto.ValidateOnlineManifest(m, online.Netease) == nil {
			t.Fatalf("invalid plugin version accepted: %q", version)
		}
	}
}
