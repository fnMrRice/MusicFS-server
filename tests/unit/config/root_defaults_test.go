package config_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/core/config"
)

func TestRootImportDefaultsYAMLValidationInheritanceAndClone(t *testing.T) {
	withRoot := func(value string) string {
		source := strings.Replace(minimalYAML+pluginYAML, "        enabled: true", "        enabled: true\n        importDefaults: {recursive: false, skipHidden: false}", 1)
		return strings.Replace(source, "              name: 中文音乐", "              name: 中文音乐\n              importDefaults: "+value, 1)
	}
	valid := withRoot("{recursive: true, maxDepth: 2, excludePatterns: ['cache']}")
	doc, err := parse(t, valid)
	if err != nil {
		t.Fatal(err)
	}
	instance := doc.Config.Plugins[0].Instances[0]
	options, err := config.EffectiveRootImportDefaults(instance, instance.Config.MusicRoots[0])
	if err != nil || !options.Recursive || options.MaxDepth != 2 || options.SkipHidden || !options.RespectNoMedia {
		t.Fatal("root defaults did not overlay instance defaults", options, err)
	}
	for _, value := range []string{"null", "{recursive: false, maxDepth: 2}", "{maxDepth: 65}", "{unknown: true}", "{importLyrics: true}"} {
		if _, err := parse(t, withRoot(value)); err == nil {
			t.Fatal("invalid root options or missing lyrics dependency accepted", value)
		}
	}
	path := filepath.Join(t.TempDir(), "server.yaml")
	if err := os.WriteFile(path, []byte(valid), 0600); err != nil {
		t.Fatal(err)
	}
	store, err := config.OpenStore(context.Background(), path, map[string]string{"ADMIN_TOKEN": "test-credential-at-least-24-characters"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := store.Current().Plugins[0].Instances[0].Config.MusicRoots[0].ImportDefaults
	*snapshot.Recursive = false
	(*snapshot.ExcludePatterns)[0] = "changed"
	current := store.Current().Plugins[0].Instances[0].Config.MusicRoots[0].ImportDefaults
	if !*current.Recursive || (*current.ExcludePatterns)[0] != "cache" {
		t.Fatal("root defaults alias a mutable configuration snapshot")
	}
}
