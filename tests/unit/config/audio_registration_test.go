package config_test

import (
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginconfig"
)

func TestAudioRolesAllowReplacementAndRejectAmbiguousActiveInstances(t *testing.T) {
	for _, item := range []struct {
		id         string
		processing bool
	}{
		{"test.decoder.first", false}, {"test.decoder.second", false}, {"test.processor.first", true}, {"test.processor.second", true},
	} {
		if err := pluginconfig.Register(pluginconfig.Declaration{ID: item.id, Role: "audio", Version: "1.0.0", AudioProcessing: item.processing, Identity: &pluginproto.PluginIdentity{ID: item.id, Name: item.id, LocalizedNames: map[string]string{}, Features: []string{"audio-decode"}}}); err != nil {
			t.Fatal(err)
		}
	}
	for _, role := range []string{"decoder", "processor"} {
		raw := minimalYAML + `plugins:
  - pluginId: test.` + role + `.first
    enabled: true
    execution: builtin
    instances:
      - instanceId: 074883fb-66af-40ac-9a02-feb45df6af4f
        enabled: true
        config: {}
  - pluginId: test.` + role + `.second
    enabled: false
    execution: builtin
    instances:
      - instanceId: 1b2c03f9-7d7b-4933-9759-b3c68c3b1f0d
        enabled: true
        config: {}
`
		if _, err := parse(t, raw); err != nil {
			t.Fatalf("replacement %s was rejected: %v", role, err)
		}
		raw = strings.Replace(raw, "enabled: false", "enabled: true", 1)
		if _, err := parse(t, raw); err == nil {
			t.Fatalf("two active %s implementations were accepted", role)
		}
	}
}
