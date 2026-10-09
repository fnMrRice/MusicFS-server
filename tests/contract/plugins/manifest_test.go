package plugins_test

import (
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/pluginproto"
)

func TestManifestRejectsUnknownRolesAndProtocol(t *testing.T) {
	base := pluginproto.Manifest{PluginID: "local-files", PluginVersion: "0.1.0", ProtocolVersion: "1.0", Roles: []string{"provider"}, MaxMessageBytes: pluginproto.MaxMessageBytes, MaxPageItems: pluginproto.MaxPageItems}
	if err := base.Validate("provider"); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*pluginproto.Manifest){
		func(m *pluginproto.Manifest) { m.ProtocolVersion = "2.0" },
		func(m *pluginproto.Manifest) { m.Roles = []string{"unknown"} },
		func(m *pluginproto.Manifest) { m.MaxMessageBytes++ },
		func(m *pluginproto.Manifest) { m.PluginID = "" },
	} {
		invalid := base
		mutate(&invalid)
		if invalid.Validate("provider") == nil {
			t.Fatal("invalid manifest was accepted")
		}
	}
}
