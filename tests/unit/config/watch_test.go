package config_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/config"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
	"go.yaml.in/yaml/v3"
)

func watchYAML(value string) string {
	return strings.Replace(minimalYAML+pluginYAML, "          musicRoots:", "          watch: "+value+"\n          musicRoots:", 1)
}

func TestWatchConfigurationSchema(t *testing.T) {
	schema := configurationSchema(t)
	for _, tc := range []struct {
		value string
		valid bool
	}{
		{"{}", true}, {"{enabled: true, debounceMs: 100, maxDelayMs: 60000, fullRescanSeconds: 86400}", true},
		{"null", false}, {"{enabled: 'true'}", false}, {"{debounceMs: 0}", false}, {"{fullRescanSeconds: 29}", false}, {"{extra: 1}", false},
	} {
		var value any
		if err := yaml.Unmarshal([]byte(watchYAML(tc.value)), &value); err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err := schema.Validate(testkit.DecodeJSON(t, raw)); (err == nil) != tc.valid {
			t.Fatalf("watch schema %s: %v", tc.value, err)
		}
	}
}

func TestWatchYAMLStrictDefaultsAndSnapshot(t *testing.T) {
	for _, value := range []string{"{}", "{enabled: true}", "{enabled: false, debounceMs: 100, maxDelayMs: 100, fullRescanSeconds: 30}"} {
		doc, err := parse(t, watchYAML(value))
		if err != nil {
			t.Fatal(err)
		}
		w := doc.Config.Plugins[0].Instances[0].Config.Watch
		if w == nil || w.FullRescanSeconds < 30 {
			t.Fatal("watch settings missing")
		}
		if value == "{}" && *w != config.DefaultWatch() {
			t.Fatal("watch defaults differ")
		}
	}
	for _, value := range []string{"null", "[]", "{unknown: PRIVATE-SENTINEL}", "{enabled: 1}", "{debounceMs: 0}", "{maxDelayMs: 999}", "{debounceMs: 30001}", "{maxDelayMs: 60001}", "{fullRescanSeconds: 29}", "{fullRescanSeconds: 86401}", "{enabled: false, debounceMs: null}"} {
		if _, err := parse(t, watchYAML(value)); err == nil || strings.Contains(err.Error(), "PRIVATE-SENTINEL") {
			t.Fatalf("invalid watch accepted or disclosed: %s: %v", value, err)
		}
	}
	if _, err := parse(t, strings.Replace(minimalYAML+pluginYAML, "config: {}", "config: {watch: {enabled: false}}", 1)); err == nil {
		t.Fatal("nonlocal watch accepted")
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(watchYAML("{enabled: true}")), 0600); err != nil {
		t.Fatal(err)
	}
	store, err := config.OpenStore(context.Background(), path, map[string]string{"ADMIN_TOKEN": "test-credential-at-least-24-characters"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := store.Snapshot()
	snapshot.Config.Plugins[0].Instances[0].Config.Watch.DebounceMs = 5000
	if store.Current().Plugins[0].Instances[0].Config.Watch.DebounceMs != 1000 {
		t.Fatal("snapshot mutated watch settings")
	}
}

func TestWatchSingleInstanceAndDisabledValidation(t *testing.T) {
	doc, err := parse(t, watchYAML("{enabled: true}"))
	if err != nil {
		t.Fatal(err)
	}
	c := doc.Config
	c.Plugins = c.Plugins[:1]
	item := c.Plugins[0].Instances[0]
	item.InstanceID = "a13edb4c-d1d6-4222-adc5-7ff4de31f45e"
	c.Plugins[0].Instances = append(c.Plugins[0].Instances, item)
	if err = c.Validate(); err == nil {
		t.Fatal("multiple local watchers were accepted")
	}
	c.Plugins[0].Enabled = false
	if err = c.Validate(); err == nil {
		t.Fatal("disabled plugins bypassed the single instance declaration")
	}
	c.Plugins[0].Instances = c.Plugins[0].Instances[:1]
	if err = c.Validate(); err != nil {
		t.Fatal("disabled listeners consume live budget", err)
	}
	c.Plugins[0].Instances[0].Config.Watch.MaxDelayMs = 1
	if err = c.Validate(); err == nil {
		t.Fatal("disabled settings bypassed validation")
	}
}
