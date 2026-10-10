package config_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/config"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func bundledDocument(t *testing.T, extra string) *config.Document {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "musicfs-plugins"), []byte("discovery does not execute binaries"), 0600); err != nil {
		t.Fatal(err)
	}
	raw := minimalYAML + "pluginBundle: './musicfs-plugins'\n" + extra
	doc, err := config.ParseDocument([]byte(raw), filepath.Join(dir, "server.yaml"), map[string]string{"ADMIN_TOKEN": "test-credential-at-least-24-characters"})
	if err != nil {
		t.Fatal(err)
	}
	if string(doc.Raw) != raw {
		t.Fatal("discovery changed the original document")
	}
	return doc
}

func TestBundledDiscoveryDefaultsAndPendingConfiguration(t *testing.T) {
	doc := bundledDocument(t, "")
	if len(doc.Config.Plugins) != 7 {
		t.Fatalf("plugins: %d", len(doc.Config.Plugins))
	}
	for _, p := range doc.Config.Plugins {
		if !p.Enabled || len(p.Instances) != 1 || !p.Instances[0].Enabled || len(p.Args) != 2 || p.Args[0] != "--plugin" {
			t.Fatalf("invalid discovery: %+v", p)
		}
		wantPending := p.PluginID == "musicfs.localfiles" || p.PluginID == "musicfs.audio.ffmpeg"
		if p.Pending(p.Instances[0]) != wantPending || p.CanStart(p.Instances[0]) == wantPending {
			t.Fatalf("readiness: %+v", p)
		}
	}
	var value any
	if err := doc.Root.Decode(&value); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := configurationSchema(t).Validate(testkit.DecodeJSON(t, encoded)); err != nil {
		t.Fatal(err)
	}
}

func TestBundledDiscoveryPreservesDisableAndExternalExecutable(t *testing.T) {
	doc := bundledDocument(t, `plugins:
  - pluginId: musicfs.ncm
    enabled: false
  - pluginId: musicfs.netease
    instances:
      - instanceId: custom-net
        role: provider
        enabled: false
        config: {}
  - pluginId: musicfs.qqmusic
    executable: './external-worker'
    args: ['literal argument with spaces', '$(not-a-shell)']
    instances: []
`)
	for _, p := range doc.Config.Plugins {
		switch p.PluginID {
		case "musicfs.ncm":
			if p.Enabled || p.CanStart(p.Instances[0]) {
				t.Fatal("explicit disable ignored")
			}
		case "musicfs.netease":
			if !p.Enabled || p.Instances[0].Enabled {
				t.Fatal("instance disable ignored")
			}
		case "musicfs.qqmusic":
			if p.Enabled || p.Args[1] != "$(not-a-shell)" || !strings.HasSuffix(p.Executable, "external-worker") {
				t.Fatal("external executable was changed")
			}
		}
	}
}

func TestPluginArgsAreStrictStringArray(t *testing.T) {
	for _, args := range []string{"'--plugin local'", "[1]", `['ok', "bad\0arg"]`} {
		raw := strings.Replace(pluginYAML, "    enabled: true", "    args: "+args+"\n    enabled: true", 1)
		if _, err := parse(t, minimalYAML+raw); err == nil {
			t.Fatalf("accepted invalid args: %s", args)
		}
	}
}
