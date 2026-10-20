package config_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/tests/testkit"
	"go.yaml.in/yaml/v3"
)

const scraperYAML = `plugins:
  - pluginId: musicfs.scraper.local
    executable: '../workers/local-scraper'
    enabled: true
    instances:
      - instanceId: d248bf34-1780-59d3-a557-9cf5d889e977
        name: 本地内容
        enabled: true
        config: {}
`

func TestScraperConfigurationRoleEnableAndStrictSettings(t *testing.T) {
	schema := configurationSchema(t)
	for name, tc := range map[string]struct {
		raw   string
		valid bool
	}{
		"enabled":             {scraperYAML, true},
		"disabled-no-program": {strings.ReplaceAll(strings.Replace(scraperYAML, "'../workers/local-scraper'", "''", 1), "enabled: true", "enabled: false"), true},
		"configured-role":     {strings.Replace(scraperYAML, "config: {}", "role: scraper\n        config: {}", 1), false},
		"active-no-program":   {strings.Replace(scraperYAML, "'../workers/local-scraper'", "''", 1), false},
		"unknown-settings":    {strings.Replace(scraperYAML, "config: {}", "config: {network: true}", 1), false},
		"unauthorized-roots":  {strings.Replace(scraperYAML, "config: {}", "config: {musicRoots: []}", 1), false},
		"import-defaults":     {strings.Replace(scraperYAML, "config: {}", "config: {}\n        importDefaults: {recursive: false}", 1), false},
		"unknown-credential":  {strings.Replace(scraperYAML, "config: {}", "config: {token: private}", 1), false},
	} {
		t.Run(name, func(t *testing.T) {
			raw := minimalYAML + tc.raw
			doc, err := parse(t, raw)
			if (err == nil) != tc.valid {
				t.Fatalf("loader: %v", err)
			}
			if err == nil && doc.Config.Plugins[0].PluginID != "musicfs.scraper.local" {
				t.Fatal("scraper identity changed")
			}
			var value any
			if err := yaml.Unmarshal([]byte(raw), &value); err != nil {
				t.Fatal(err)
			}
			body, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			if err := schema.Validate(testkit.DecodeJSON(t, body)); (err == nil) != tc.valid {
				t.Fatalf("schema: %v", err)
			}
		})
	}
}
