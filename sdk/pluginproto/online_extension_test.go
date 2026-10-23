package pluginproto_test

import (
	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"testing"
)

func TestOnlineCapabilitiesAndLoginBelongToPluginDeclaration(t *testing.T) {
	m, d := compatibleOnlinePlugin()
	d.Platform = "independent-catalog"
	d.Plugin.ID = pluginproto.OnlinePluginID(d.Platform)
	d.Authentication = "anonymous"
	d.Resources.Albums = []online.ReferenceRule{{IDPattern: `^([a-z0-9_]+)$`, URLTemplate: "https://catalog.example.org/album/${1}"}}
	d.Capabilities = online.DeclareCapabilities("metadata.album", "stream.resolve")
	m.PluginID, m.Plugin, m.Capabilities = d.Plugin.ID, d.Plugin.Clone(), pluginproto.OnlineCapabilities(d)
	if err := pluginproto.ValidateOnlineDescriptor(m, d, d.Platform); err != nil {
		t.Fatal("new plugin capabilities require host platform edits", err)
	}
	d.Login = &online.LoginDescriptor{Method: "deviceCode", Validation: "verified"}
	if err := pluginproto.ValidateOnlineDescriptor(m, d, d.Platform); err != nil {
		t.Fatal("plugin-owned login declaration rejected", err)
	}
	m.Capabilities = append(m.Capabilities, "filesystem.write")
	if pluginproto.ValidateOnlineDescriptor(m, d, d.Platform) == nil {
		t.Fatal("unsupported protocol method accepted")
	}
}
