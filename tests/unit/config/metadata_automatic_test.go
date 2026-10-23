package config_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
	"go.yaml.in/yaml/v3"
)

func TestAutomaticMetadataConfigurationDefaultsAndStrictPolicy(t *testing.T) {
	doc, err := parse(t, minimalYAML)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Config.Metadata.Automatic.Enabled || !reflect.DeepEqual(doc.Config.Metadata.Automatic.Fields, []string{"lyrics", "artwork"}) {
		t.Fatal("automatic metadata defaults changed")
	}
	schema := configurationSchema(t)
	for name, tc := range map[string]struct {
		policy string
		valid  bool
	}{
		"disabled":               {"{enabled: false}", true},
		"enabled-default-fields": {"{enabled: true}", true},
		"explicit":               {"{enabled: true, instancePriority: [d248bf34-1780-59d3-a557-9cf5d889e977], fields: [lyrics], languages: [zh, und]}", true},
		"disabled-empty-fields":  {"{enabled: false, fields: []}", true},
		"enabled-empty-fields":   {"{enabled: true, fields: []}", false},
		"unknown-field":          {"{enabled: true, privateSetting: true}", false},
		"null-policy":            {"null", false},
		"null-priority":          {"{instancePriority: null}", false},
		"null-languages":         {"{languages: null}", false},
		"quoted-enabled":         {"{enabled: 'true'}", false},
		"duplicate-priority":     {"{instancePriority: [d248bf34-1780-59d3-a557-9cf5d889e977, d248bf34-1780-59d3-a557-9cf5d889e977]}", false},
		"too-many-priority":      {"{instancePriority: [a,b,c,d,e]}", false},
		"bad-fields":             {"{fields: [title]}", false},
		"duplicate-fields":       {"{fields: [lyrics, lyrics]}", false},
		"bad-language":           {"{languages: ['zh_CN']}", false},
		"duplicate-language":     {"{languages: [zh, zh]}", false},
	} {
		t.Run(name, func(t *testing.T) {
			raw := minimalYAML + scraperYAML + "metadata:\n  automatic: " + tc.policy + "\n"
			_, err := parse(t, raw)
			if (err == nil) != tc.valid {
				t.Fatalf("loader valid=%v: %v", tc.valid, err)
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
				t.Fatalf("schema valid=%v: %v", tc.valid, err)
			}
		})
	}
}

func TestAutomaticMetadataDependenciesAndSnapshotIsolation(t *testing.T) {
	combined := minimalYAML + pluginYAML + strings.TrimPrefix(scraperYAML, "plugins:\n")
	policy := "metadata:\n  automatic: {enabled: true, instancePriority: [d248bf34-1780-59d3-a557-9cf5d889e977], fields: [lyrics, artwork], languages: [ZH, und]}\n"
	for name, raw := range map[string]string{
		"no-scraper":              minimalYAML + "plugins: []\n" + policy,
		"wrong-priority":          combined + strings.Replace(policy, "[d248bf34-1780-59d3-a557-9cf5d889e977]", "[local]", 1),
		"unknown-priority":        combined + strings.Replace(policy, "[d248bf34-1780-59d3-a557-9cf5d889e977]", "[missing]", 1),
		"case-duplicate-language": combined + strings.Replace(policy, "[ZH, und]", "[ZH, zh]", 1),
		"import-without-scraper":  strings.Replace(minimalYAML+pluginYAML, "importLyrics: false", "importLyrics: true", 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parse(t, raw); err == nil {
				t.Fatal("invalid policy dependency accepted")
			}
		})
	}
	importOnly := strings.Replace(combined, "importLyrics: false", "importLyrics: true", 1)
	if _, err := parse(t, importOnly); err != nil {
		t.Fatalf("explicit lyrics import requires no global automatic switch: %v", err)
	}
	path := filepath.Join(t.TempDir(), "policy.yaml")
	if err := os.WriteFile(path, []byte(combined+policy), 0600); err != nil {
		t.Fatal(err)
	}
	store, err := config.OpenStore(context.Background(), path, map[string]string{"ADMIN_TOKEN": "test-credential-at-least-24-characters"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := store.Current().Metadata.Automatic
	for _, cfg := range []config.Config{store.Current(), store.Snapshot().Config} {
		cfg.Metadata.Automatic.InstancePriority[0] = "mutated"
		cfg.Metadata.Automatic.Fields[0] = "mutated"
		cfg.Metadata.Automatic.Languages[0] = "mutated"
	}
	if !reflect.DeepEqual(store.Current().Metadata.Automatic, want) || !reflect.DeepEqual(store.Snapshot().Config.Metadata.Automatic, want) {
		t.Fatal("returned configuration mutated loaded automatic policy")
	}
}
