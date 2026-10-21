package config_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/core/config"
)

func TestExclusionYAMLValidationAndSnapshotIsolation(t *testing.T) {
	withRules := func(value string) string {
		return strings.Replace(minimalYAML+pluginYAML, "      - instanceId: b1548e96-3081-5c9c-8376-bf290e369d51\n", "      - instanceId: b1548e96-3081-5c9c-8376-bf290e369d51\n        importDefaults: {excludePatterns: "+value+"}\n", 1)
	}
	for _, value := range []string{`['**/cache', '*.tmp']`, `[]`} {
		doc, err := parse(t, withRules(value))
		if err != nil {
			t.Fatal(err)
		}
		if _, err = config.EffectiveImportDefaults("musicfs.plugin.source.localfiles", doc.Config.Plugins[0].Instances[0]); err != nil {
			t.Fatal(err)
		}
	}
	for _, value := range []string{`null`, `['../PRIVATE-SENTINEL']`, `['a', 'a']`, `[1]`, `'*.tmp'`, `['a/**b']`} {
		_, err := parse(t, withRules(value))
		if err == nil || strings.Contains(err.Error(), "PRIVATE-SENTINEL") {
			t.Fatalf("invalid rules accepted or disclosed: %s %v", value, err)
		}
	}
	file := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(file, []byte(withRules(`['cache']`)), 0600); err != nil {
		t.Fatal(err)
	}
	store, err := config.OpenStore(context.Background(), file, map[string]string{"ADMIN_TOKEN": "test-credential-at-least-24-characters"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := store.Snapshot()
	(*snapshot.Config.Plugins[0].Instances[0].ImportDefaults.ExcludePatterns)[0] = "changed"
	if (*store.Current().Plugins[0].Instances[0].ImportDefaults.ExcludePatterns)[0] != "cache" {
		t.Fatal("snapshot mutated active exclusions")
	}
}
