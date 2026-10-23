package config_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func bundledDocument(t *testing.T, extra string) *config.Document {
	t.Helper()
	return discoveredDocument(t, extra, []string{"musicfs-plugins", "musicfs-plugin-netease", "musicfs-plugin-qqmusic", "musicfs-plugin-applemusic", "musicfs-plugin-bilibili"})
}

func discoveredDocument(t *testing.T, extra string, files []string) *config.Document {
	t.Helper()
	dir := t.TempDir()
	for _, name := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("discovery does not execute binaries"), 0600); err != nil {
			t.Fatal(err)
		}
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
	if len(doc.Config.Plugins) != 8 {
		t.Fatalf("plugins: %d", len(doc.Config.Plugins))
	}
	for _, p := range doc.Config.Plugins {
		if !p.Enabled || len(p.Instances) != 1 || !p.Instances[0].Enabled {
			t.Fatalf("invalid discovery: %+v", p)
		}
		if !p.InProcess() || p.Executable != "" || len(p.Args) != 0 {
			t.Fatalf("builtin discovery requires an external program: %+v", p)
		}
		wantPending := p.PluginID == "musicfs.plugin.source.localfiles" || p.PluginID == "musicfs.plugin.decoder.ffmpeg"
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
	if strings.Contains(string(encoded), `"name":`) {
		t.Fatal("discovery populated an instance alias with a plugin display name")
	}
	if strings.Contains(string(encoded), `"role":`) {
		t.Fatal("discovery persisted plugin roles as deployment configuration")
	}
	if err := configurationSchema(t).Validate(testkit.DecodeJSON(t, encoded)); err != nil {
		t.Fatal(err)
	}
}

func TestBundledDiscoveryPreservesDisableAndExternalExecutable(t *testing.T) {
	doc := bundledDocument(t, `plugins:
  - pluginId: musicfs.plugin.decoder.ncm
    enabled: false
  - pluginId: musicfs.plugin.source.netease
    instances:
      - instanceId: custom-net
        name: Upstairs collection
        enabled: false
        config: {}
  - pluginId: musicfs.plugin.source.qqmusic
    execution: process
    executable: './external-worker'
    args: ['literal argument with spaces', '$(not-a-shell)']
    instances: []
`)
	for _, p := range doc.Config.Plugins {
		switch p.PluginID {
		case "musicfs.plugin.decoder.ncm":
			if p.Enabled || p.CanStart(p.Instances[0]) {
				t.Fatal("explicit disable ignored")
			}
		case "musicfs.plugin.source.netease":
			if p.Enabled || p.Instances[0].Enabled || p.Instances[0].Name != "Upstairs collection" {
				t.Fatal("explicit instance settings were changed")
			}
		case "musicfs.plugin.source.qqmusic":
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
