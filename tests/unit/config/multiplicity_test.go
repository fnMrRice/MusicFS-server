package config_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/tests/testkit"
	"go.yaml.in/yaml/v3"
)

func TestSingleInstancePluginsCountDisabledConfigurations(t *testing.T) {
	schema := configurationSchema(t)
	for _, pluginID := range []string{"musicfs.plugin.source.localfiles", "musicfs.plugin.decoder.ffmpeg", "musicfs.plugin.decoder.ncm"} {
		t.Run(pluginID, func(t *testing.T) {
			raw := minimalYAML + fmt.Sprintf(`plugins:
  - pluginId: %s
    enabled: false
    instances:
      - instanceId: 3bdd4699-d687-43f4-a96f-1ff50d5b2cce
        enabled: false
        config: {}
      - instanceId: a13edb4c-d1d6-4222-adc5-7ff4de31f45e
        enabled: false
        config: {}
`, pluginID)
			if _, err := parse(t, raw); err == nil || !strings.Contains(err.Error(), "at most one configured instance") {
				t.Fatalf("disabled duplicate instance was accepted: %v", err)
			}
			var value any
			if err := yaml.Unmarshal([]byte(raw), &value); err != nil {
				t.Fatal(err)
			}
			body, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			if schema.Validate(testkit.DecodeJSON(t, body)) == nil {
				t.Fatal("configuration schema accepted disabled duplicate instances")
			}
		})
	}
}
