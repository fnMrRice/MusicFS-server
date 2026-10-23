package plugins_test

import (
	"encoding/json"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginconfig"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestManifestSourceTypeBelongsToProviderDeclaration(t *testing.T) {
	for _, role := range []string{"provider", "audio", "scraper"} {
		for _, source := range []string{"", "local", "network", "bilibili"} {
			manifest := pluginproto.Manifest{PluginID: "musicfs.plugin.source.localfiles", Plugin: pluginconfig.Identity("musicfs.plugin.source.localfiles"), PluginVersion: "0.1.0", ProtocolVersion: "1.0", Roles: []string{role}, SourceType: source, Capabilities: []string{}, Systems: []string{"windows"}, Architectures: []string{"amd64"}, MaxMessageBytes: pluginproto.MaxMessageBytes, MaxPageItems: pluginproto.MaxPageItems}
			valid := role == "provider" && (source == "local" || source == "network") || role != "provider" && source == ""
			data, err := json.Marshal(manifest)
			if err != nil {
				t.Fatal(err)
			}
			if schemaErr := testkit.ContractSchema(t, "plugin", "Manifest").Validate(testkit.DecodeJSON(t, data)); (schemaErr == nil) != valid {
				t.Fatalf("role=%s source=%q schema validity mismatch: %v", role, source, schemaErr)
			}
			if (manifest.Validate(role) == nil) != valid {
				t.Fatalf("role=%s source=%q host validity mismatch", role, source)
			}
		}
	}
}
