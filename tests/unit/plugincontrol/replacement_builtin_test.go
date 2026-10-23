package plugincontrol_test

import (
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugincontrol"
)

func TestReplacementBuiltinInventoryUsesActualAssemblyDeclarations(t *testing.T) {
	declarations := []plugincontrol.BuiltinDeclaration{
		{PluginID: "example.analysis.alternative", Identity: &pluginproto.PluginIdentity{ID: "example.analysis.alternative", Name: "Alternative Analyzer", LocalizedNames: map[string]string{}, Features: []string{"audio-analysis"}}, Version: "4.0.0", Capabilities: []string{"fingerprint.alternative"}, Configured: func(c config.Config) bool { return c.Fingerprinting.Enabled }},
		{PluginID: "example.tags.alternative", Identity: &pluginproto.PluginIdentity{ID: "example.tags.alternative", Name: "Alternative Tags", LocalizedNames: map[string]string{}, Features: []string{"metadata"}}, Version: "2.0.0", Capabilities: []string{"tags.alternative.rewrite"}, Configured: func(c config.Config) bool { return c.WriteBack.Enabled }},
	}
	cfg := config.Config{Fingerprinting: config.FingerprintingConfig{Enabled: true}}
	m, err := plugincontrol.NewWithBuiltins(t.Context(), cfg, nil, nil, declarations)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	declarations[0].Capabilities[0] = "mutated"
	declarations[0].Identity.Name = "mutated"
	items := map[string]plugincontrol.Plugin{}
	for _, p := range m.Snapshot().Items {
		items[p.PluginID] = p
	}
	for _, id := range []string{"musicfs.plugin.analysis.chromaprint", "musicfs.plugin.tags.flac"} {
		if _, exists := items[id]; exists {
			t.Fatal("replacement assembly still reports an uninstalled algorithm", id)
		}
	}
	analyzer := items["example.analysis.alternative"]
	if analyzer.Plugin.Name != "Alternative Analyzer" || analyzer.Version != "4.0.0" || analyzer.Builtin == nil || !analyzer.Builtin.Configured || analyzer.Builtin.Capabilities[0] != "fingerprint.alternative" {
		t.Fatalf("replacement analyzer declaration was lost: %+v", analyzer)
	}
	tags := items["example.tags.alternative"]
	if tags.Version != "2.0.0" || tags.Builtin == nil || tags.Builtin.Configured || tags.Builtin.Capabilities[0] != "tags.alternative.rewrite" {
		t.Fatalf("replacement tag format declaration was lost: %+v", tags)
	}
	analyzer.Plugin.Name, analyzer.Builtin.Capabilities[0] = "snapshot mutation", "snapshot mutation"
	if again := managedItem(t, m, analyzer.PluginID); again.Plugin.Name != "Alternative Analyzer" || again.Builtin.Capabilities[0] != "fingerprint.alternative" {
		t.Fatal("managed inventory shares mutable declarations")
	}
	if _, err = m.Change(t.Context(), "admin", config.Change{PluginID: analyzer.PluginID, Enabled: false}); err == nil || pluginproto.ErrorCode(err) != "UNSUPPORTED" {
		t.Fatal("replacement builtin accepted instance state mutation", err)
	}
}

func TestBuiltinAssemblyRejectsDuplicateAndInvalidDeclarations(t *testing.T) {
	valid := plugincontrol.BuiltinDeclaration{PluginID: "example.tags.valid", Identity: &pluginproto.PluginIdentity{ID: "example.tags.valid", Name: "Tags", LocalizedNames: map[string]string{}, Features: []string{"metadata"}}, Version: "1.0.0", Capabilities: []string{"tags.rewrite"}, Configured: func(config.Config) bool { return true }}
	invalid := valid
	invalid.Version = ""
	for _, declarations := range [][]plugincontrol.BuiltinDeclaration{{valid, valid}, {invalid}} {
		if manager, err := plugincontrol.NewWithBuiltins(t.Context(), config.Config{}, nil, nil, declarations); err == nil {
			manager.Close()
			t.Fatal("invalid builtin assembly accepted")
		}
	}
}
