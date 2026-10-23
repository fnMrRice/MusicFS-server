package plugins_test

import (
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginconfig"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestManifestRejectsUnknownRolesAndProtocol(t *testing.T) {
	base := pluginproto.Manifest{PluginID: "musicfs.plugin.source.localfiles", Plugin: pluginconfig.Identity("musicfs.plugin.source.localfiles"), PluginVersion: "0.1.0", ProtocolVersion: "1.0", SourceType: pluginproto.SourceLocal, Roles: []string{"provider"}, MaxMessageBytes: pluginproto.MaxMessageBytes, MaxPageItems: pluginproto.MaxPageItems}
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

func TestEveryManifestRequiresBoundedSemanticPluginVersion(t *testing.T) {
	for _, role := range []string{"provider", "audio", "scraper"} {
		manifest := pluginproto.Manifest{PluginID: "musicfs.plugin.source.localfiles", Plugin: pluginconfig.Identity("musicfs.plugin.source.localfiles"), PluginVersion: "2.3.4-rc.1+build.9", ProtocolVersion: pluginproto.ProtocolVersion, Roles: []string{role}, Capabilities: []string{}, Systems: []string{"windows"}, Architectures: []string{"amd64"}, MaxMessageBytes: pluginproto.MaxMessageBytes, MaxPageItems: pluginproto.MaxPageItems}
		if role == "provider" {
			manifest.SourceType = pluginproto.SourceLocal
		}
		if err := manifest.Validate(role); err != nil {
			t.Fatal(err)
		}
		testkit.ValidatePlugin(t, "Manifest", manifest)
		for _, version := range []string{"", " ", "\t", "1.0", "01.0.0", "1.0.0-01", "1.0.0+", "1.0.0+" + strings.Repeat("a", 123)} {
			manifest.PluginVersion = version
			if manifest.Validate(role) == nil {
				t.Fatalf("%s manifest accepted invalid plugin version: %q", role, version)
			}
			value := map[string]any{"pluginVersion": version}
			if testkit.ContractSchema(t, "plugin", "Version").Validate(value["pluginVersion"]) == nil {
				t.Fatalf("schema accepted invalid plugin version: %q", version)
			}
		}
	}
}
