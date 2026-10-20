package config_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/tests/testkit"
	"go.yaml.in/yaml/v3"
)

func TestOnlineConfigurationAggregateInstanceLimit(t *testing.T) {
	raw := "plugins:\n"
	for p, platform := range []string{"netease", "qqmusic"} {
		raw += "  - pluginId: musicfs.plugin.source." + platform + "\n    instances:\n"
		for i := range 8 {
			raw += fmt.Sprintf("      - instanceId: online-%d-%d\n        config: {}\n", p, i)
		}
	}
	if _, err := parse(t, minimalYAML+raw); err != nil {
		t.Fatal("sixteen disabled instances rejected", err)
	}
	raw += "      - instanceId: excess\n        config: {}\n"
	if _, err := parse(t, minimalYAML+raw); err == nil {
		t.Fatal("aggregate online instance budget was exceeded")
	}
}

func TestOnlineConfigurationFixedPlatformsAndEmptySettings(t *testing.T) {
	schema := configurationSchema(t)
	for _, platform := range []string{"netease", "qqmusic", "applemusic"} {
		raw := strings.ReplaceAll(scraperYAML, "musicfs.scraper.local", "musicfs.plugin.source."+platform)
		for name, tc := range map[string]struct {
			value string
			valid bool
		}{
			"enabled":         {raw, true},
			"disabled":        {strings.ReplaceAll(raw, "enabled: true", "enabled: false"), true},
			"configured-role": {strings.Replace(raw, "config: {}", "role: provider\n        config: {}", 1), false},
			"url":             {strings.Replace(raw, "config: {}", "config: {url: 'http://127.0.0.1/private'}", 1), false},
			"token":           {strings.Replace(raw, "config: {}", "config: {token: secret}", 1), false},
			"account":         {strings.Replace(raw, "config: {}", "config: {accountId: someone}", 1), false},
			"roots":           {strings.Replace(raw, "config: {}", "config: {musicRoots: []}", 1), false},
			"import-defaults": {strings.Replace(raw, "config: {}", "config: {}\n        importDefaults: {}", 1), false},
			"missing-program": {strings.Replace(raw, "'../workers/local-scraper'", "''", 1), false},
		} {
			t.Run(platform+"/"+name, func(t *testing.T) {
				input := minimalYAML + tc.value
				if _, err := parse(t, input); (err == nil) != tc.valid {
					t.Fatalf("loader: %v", err)
				}
				var value any
				if err := yaml.Unmarshal([]byte(input), &value); err != nil {
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
}
