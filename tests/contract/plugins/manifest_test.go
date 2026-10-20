package plugins_test

import (
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/pluginconfig"
	"github.com/fnMrRice/MusicFS-server/src/pluginproto"
)

func TestManifestRejectsUnknownRolesAndProtocol(t *testing.T) {
	base := pluginproto.Manifest{PluginID: "musicfs.plugin.source.localfiles", Plugin: pluginconfig.Identity("musicfs.plugin.source.localfiles"), PluginVersion: "0.1.0", ProtocolVersion: "1.0", Roles: []string{"provider"}, MaxMessageBytes: pluginproto.MaxMessageBytes, MaxPageItems: pluginproto.MaxPageItems}
	if err := base.Validate("provider"); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*pluginproto.Manifest){
		func(m *pluginproto.Manifest) { m.ProtocolVersion = "2.0" },
		func(m *pluginproto.Manifest) { m.Roles = []string{"unknown"} },
		func(m *pluginproto.Manifest) { m.Roles = []string{"audio"} },
		func(m *pluginproto.Manifest) { m.Roles = nil },
		func(m *pluginproto.Manifest) { m.MaxMessageBytes++ },
		func(m *pluginproto.Manifest) { m.PluginID = "" },
		func(m *pluginproto.Manifest) { m.Plugin = nil },
		func(m *pluginproto.Manifest) { m.Plugin = m.Plugin.Clone(); m.Plugin.Name = "" },
		func(m *pluginproto.Manifest) { m.Plugin = m.Plugin.Clone(); m.Plugin.LocalizedNames = nil },
	} {
		invalid := base
		mutate(&invalid)
		if invalid.Validate("provider") == nil {
			t.Fatal("invalid manifest was accepted")
		}
	}
}
