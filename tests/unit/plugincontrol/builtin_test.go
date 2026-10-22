package plugincontrol_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugincontrol"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestBuiltinAlgorithmInventoryDescribesConfigurationWithoutRuntimeClaims(t *testing.T) {
	cfg := config.Config{Fingerprinting: config.FingerprintingConfig{Enabled: true, FpcalcPath: "private-tool-path", FpcalcSHA256: "private-digest"}}
	m, err := plugincontrol.New(t.Context(), cfg, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	for id, enabled := range map[string]bool{
		"musicfs.plugin.analysis.chromaprint": true,
		"musicfs.plugin.tags.flac":            false,
		"musicfs.plugin.matching.standard":    true,
		"musicfs.plugin.transform.thumbnail":  true,
	} {
		p := managedItem(t, m, id)
		if p.Builtin == nil || p.Builtin.Configured != enabled || p.ConfiguredEnabled != enabled || p.Execution != "builtin" || p.StateMutable || p.Required || len(p.Instances) != 0 || len(p.Builtin.Capabilities) != 1 {
			t.Fatalf("invalid builtin declaration: %+v", p)
		}
		if err := p.Plugin.Validate(id); err != nil {
			t.Fatal(err)
		}
		if capability := map[string]string{
			"musicfs.plugin.tags.flac":           "tags.flac.rewrite",
			"musicfs.plugin.transform.thumbnail": "artwork.thumbnail256",
		}[id]; capability != "" {
			if len(p.Plugin.Features) != 1 || p.Plugin.Features[0] != "metadata" || p.Builtin.Capabilities[0] != capability {
				t.Fatalf("metadata inventory lost its classification or specific capability: %+v", p)
			}
		}
		encoded, err := json.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		testkit.ValidateHTTP(t, "ManagedPlugin", encoded)
		for _, requested := range []bool{true, false} {
			if _, err := m.Change(t.Context(), "admin", config.Change{PluginID: id, Enabled: requested}); err == nil {
				t.Fatal("algorithm accepted an unrelated instance state operation")
			}
		}
		p.Builtin.Capabilities[0] = "changed-by-reader"
		if managedItem(t, m, id).Builtin.Capabilities[0] == "changed-by-reader" {
			t.Fatal("snapshot aliases builtin metadata")
		}
	}
	body, err := json.Marshal(m.Snapshot())
	if err != nil || strings.Contains(string(body), "private-") || strings.Contains(string(body), `"runtimeState":"ready"`) {
		t.Fatal("configuration exposed tool details or claimed runtime readiness", string(body), err)
	}
}
