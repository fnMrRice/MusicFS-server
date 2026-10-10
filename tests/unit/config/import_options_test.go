package config_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/config"
	"github.com/fnMrRice/MusicFS-server/src/pluginconfig"
	"github.com/fnMrRice/MusicFS-server/src/pluginproto"
)

func TestImportOverridesNormalizeEachLayer(t *testing.T) {
	base := pluginconfig.LocalFiles().ImportDefaults
	for _, tc := range []struct {
		name, first, second string
		recursive           bool
		depth               int
		valid               bool
	}{
		{"defaults", `{}`, `{}`, true, 64, true},
		{"disable", `{}`, `{"recursive":false}`, false, 0, true},
		{"enable resets", `{"recursive":false}`, `{"recursive":true}`, true, 64, true},
		{"explicit depth", `{"recursive":false}`, `{"recursive":true,"maxDepth":3}`, true, 3, true},
		{"keep inherited", `{"maxDepth":8}`, `{"recursive":true}`, true, 8, true},
		{"contradiction", `{}`, `{"recursive":false,"maxDepth":1}`, false, 0, false},
		{"zero recursive", `{}`, `{"maxDepth":0}`, false, 0, false},
		{"inherited false", `{"recursive":false}`, `{"maxDepth":3}`, false, 0, false},
		{"limit", `{}`, `{"maxDepth":65}`, false, 0, false},
		{"negative", `{}`, `{"maxDepth":-1}`, false, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var first, second pluginproto.ImportOverrides
			if err := json.Unmarshal([]byte(tc.first), &first); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(tc.second), &second); err != nil {
				t.Fatal(err)
			}
			layer, err := pluginproto.MergeImportOptions(base, first)
			if err != nil {
				t.Fatal(err)
			}
			result, err := pluginproto.MergeImportOptions(layer, second)
			if (err == nil) != tc.valid {
				t.Fatalf("unexpected validity: %v", err)
			}
			if tc.valid && (result.Recursive != tc.recursive || result.MaxDepth != tc.depth) {
				t.Fatalf("wrong options: %+v", result)
			}
		})
	}
	for _, raw := range []string{`null`, `[]`, `{"recursive":null}`, `{"maxDepth":null}`, `{"importLyrics":null}`, `{"maxDepth":1.5}`, `{"recursive":"false"}`, `{"privatePath":"secret"}`} {
		var patch pluginproto.ImportOverrides
		if json.Unmarshal([]byte(raw), &patch) == nil {
			t.Fatalf("invalid override accepted: %s", raw)
		}
	}
}

func TestInstanceImportDefaultsStrictAndCloned(t *testing.T) {
	withDefaults := func(value string) string {
		return strings.Replace(minimalYAML+pluginYAML, "        role: provider\n", "        role: provider\n        importDefaults: "+value+"\n", 1)
	}
	for _, value := range []string{"{recursive: false}", "{recursive: true, maxDepth: 1}", "{maxDepth: 64}", "{}"} {
		doc, err := parse(t, withDefaults(value))
		if err != nil {
			t.Fatal(err)
		}
		if _, err = config.EffectiveImportDefaults("musicfs.localfiles", doc.Config.Plugins[0].Instances[0]); err != nil {
			t.Fatal(err)
		}
	}
	for _, value := range []string{"null", "{recursive: null}", "{maxDepth: '3'}", "{maxDepth: 0}", "{maxDepth: 65}", "{recursive: false, maxDepth: 2}", "{importLyrics: true}", "{privatePath: PRIVATE-SENTINEL}"} {
		_, err := parse(t, withDefaults(value))
		if err == nil || strings.Contains(err.Error(), "PRIVATE-SENTINEL") {
			t.Fatalf("invalid import defaults accepted or disclosed: %s %v", value, err)
		}
	}
	wrong := strings.Replace(minimalYAML+pluginYAML, "      - instanceId: ncm\n", "      - instanceId: ncm\n        importDefaults: {}\n", 1)
	if _, err := parse(t, wrong); err == nil {
		t.Fatal("audio plugin accepted import defaults")
	}
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(withDefaults("{recursive: false}")), 0600); err != nil {
		t.Fatal(err)
	}
	store, err := config.OpenStore(context.Background(), path, map[string]string{"ADMIN_TOKEN": "test-credential-at-least-24-characters"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := store.Snapshot()
	*snapshot.Config.Plugins[0].Instances[0].ImportDefaults.Recursive = true
	if *store.Current().Plugins[0].Instances[0].ImportDefaults.Recursive {
		t.Fatal("mutable snapshot changed loaded defaults")
	}
}
