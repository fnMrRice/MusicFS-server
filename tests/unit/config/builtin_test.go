package config_test

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/instanceid"
	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
	"go.yaml.in/yaml/v3"
)

func TestExplicitPluginListKeepsSelectionAndAccountSettings(t *testing.T) {
	raw := `plugins:
  - pluginId: musicfs.plugin.source.netease
    executable: './old-online-worker-must-not-run'
    args: ['--plugin', 'netease']
    instances:
      - instanceId: existing-account
        ownerUserId: alice
        name: Saved account
        config: {}
`
	for _, discover := range []bool{false, true} {
		prefix := minimalYAML
		if discover {
			prefix += "pluginBundle: './missing-bundle'\n"
		}
		doc, err := parse(t, prefix+raw)
		if err != nil {
			t.Fatal(err)
		}
		p := discoveredPlugin(t, doc, "musicfs.plugin.source.netease")
		if !p.InProcess() || p.Enabled || len(p.Instances) != 1 {
			t.Fatalf("old plugin selection changed: %+v", p)
		}
		i := p.Instances[0]
		if i.Enabled || i.InstanceID != instanceid.Canonical("existing-account") || i.OwnerUserID != "alice" || i.Name != "Saved account" {
			t.Fatalf("saved account settings changed: %+v", i)
		}
		if !discover && len(doc.Config.Plugins) != 1 {
			t.Fatal("explicit selection silently added accounts or dependencies")
		}
	}
	for _, raw := range []string{"plugins: []\n", "plugins: []\nmetadata: {automatic: {enabled: false}}\n"} {
		doc, err := parse(t, minimalYAML+raw)
		if err != nil || len(doc.Config.Plugins) != 0 {
			t.Fatalf("explicit empty selection was expanded: %v", err)
		}
	}
}

func TestBuiltinReadinessDoesNotRequirePluginExecutables(t *testing.T) {
	raw := `plugins:
  - pluginId: musicfs.plugin.decoder.ncm
    enabled: true
    instances:
      - instanceId: d1971de9-e153-57c6-ac53-82dfb631da0b
        enabled: true
        config: {}
  - pluginId: musicfs.plugin.source.netease
    execution: builtin
    enabled: true
    instances:
      - instanceId: a5e47aac-f949-5e45-9709-f28a0d8443df
        enabled: true
        config: {}
  - pluginId: musicfs.plugin.source.localfiles
    enabled: true
    instances:
      - instanceId: b1548e96-3081-5c9c-8376-bf290e369d51
        enabled: true
        config: {}
  - pluginId: musicfs.plugin.decoder.ffmpeg
    enabled: true
    instances:
      - instanceId: 7edbaa88-7094-5657-ac43-37c118ef4f77
        enabled: true
        config: {}
`
	doc, err := parse(t, minimalYAML+raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Config.Plugins) != 4 {
		t.Fatal("explicit builtin selection was expanded")
	}
	for _, p := range doc.Config.Plugins {
		wantPending := p.PluginID == "musicfs.plugin.source.localfiles" || p.PluginID == "musicfs.plugin.decoder.ffmpeg"
		if !p.InProcess() || p.Executable != "" || len(p.Args) != 0 || p.Pending(p.Instances[0]) != wantPending || p.CanStart(p.Instances[0]) == wantPending {
			t.Fatalf("builtin readiness confused tools with plugin programs: %+v", p)
		}
	}
	for _, mode := range []string{"process", "unknown"} {
		changed := strings.Replace(raw, "pluginId: musicfs.plugin.decoder.ncm\n", "pluginId: musicfs.plugin.decoder.ncm\n    execution: "+mode+"\n", 1)
		for _, bundle := range []string{"", "pluginBundle: ''\n"} {
			if _, err := parse(t, minimalYAML+bundle+changed); err == nil {
				t.Fatal("invalid external execution was accepted")
			}
		}
	}
	var input any
	if err := yaml.Unmarshal([]byte(minimalYAML+raw), &input); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(input)
	if err := configurationSchema(t).Validate(testkit.DecodeJSON(t, body)); err != nil {
		t.Fatalf("editor schema rejected builtin configuration: %v", err)
	}
}

func TestOmittedPluginListDiscoversBuiltinCatalogWithoutFiles(t *testing.T) {
	doc, err := parse(t, minimalYAML)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Config.Plugins) != 8 {
		t.Fatal("builtin catalog was not discovered")
	}
	for _, p := range doc.Config.Plugins {
		if !p.InProcess() || p.Executable != "" || len(p.Args) != 0 || !p.Enabled || len(p.Instances) != 1 || !p.Instances[0].Enabled {
			t.Fatalf("discovered builtin needs a plugin process: %+v", p)
		}
	}
}

func TestOfficialSQLiteAndGainMigrateInProcess(t *testing.T) {
	for _, executable := range []string{"musicfs-plugin-storage-sqlite", "musicfs-plugin-sqlite.exe", "musicfs-plugins"} {
		args := "[]"
		if executable == "musicfs-plugins" {
			args = "['--plugin', 'storage-sqlite']"
		}
		raw := minimalYAML + "plugins: []\nstorage: {backend: plugin, executable: './" + executable + "', sha256: '" + strings.Repeat("a", 64) + "', args: " + args + ", options: {saved: value}}\n" +
			"audioDSP: {plugins: [{name: musicfs.dsp.gain, executable: './missing-dsp', sha256: '" + strings.Repeat("b", 64) + "'}]}\n"
		doc, err := parse(t, raw)
		if err != nil {
			t.Fatal(err)
		}
		s := doc.Config.Storage
		if s.Backend != "sqlite" || s.Executable != "" || s.SHA256 != "" || len(s.Args) != 0 || s.Options["saved"] != "value" {
			t.Fatalf("official SQLite did not migrate: %+v", s)
		}
		gain := doc.Config.AudioDSP.Plugins[0]
		if !gain.Builtin || gain.VerifyExecutable() != nil || doc.Config.PluginBundle != "" {
			t.Fatal("official DSP still requires its old worker executable")
		}
		if string(doc.Raw) != raw {
			t.Fatal("runtime migration rewrote the source document")
		}
	}
}

func TestSQLiteExplicitExternalModeAndCustomBackendRemainExternal(t *testing.T) {
	for _, tc := range []struct{ executable, execution, args string }{
		{"musicfs-plugin-storage-sqlite", "process", "[]"},
		{"musicfs-plugins", "process", "['--plugin', 'storage-sqlite']"},
		{"custom-storage", "", "[]"},
		{"musicfs-plugins", "", "['--plugin', 'netease']"},
	} {
		mode := ""
		if tc.execution != "" {
			mode = ", execution: " + tc.execution
		}
		raw := minimalYAML + "plugins: []\nstorage: {backend: plugin, executable: './" + tc.executable + "', sha256: '" + strings.Repeat("a", 64) + "', args: " + tc.args + mode + "}\n"
		doc, err := parse(t, raw)
		if err != nil {
			t.Fatal(err)
		}
		if doc.Config.Storage.Backend != "plugin" || filepath.Base(doc.Config.Storage.Executable) != tc.executable || config.PluginDatabaseBackend(doc.Config).Name != "plugin" {
			t.Fatal("explicit external backend was silently replaced")
		}
	}
}

func TestStorageExecutionMatchesEffectiveBackend(t *testing.T) {
	digest := strings.Repeat("a", 64)
	for _, tc := range []struct {
		name, settings string
		valid          bool
	}{
		{"custom worker cannot claim builtin", "{backend: plugin, execution: builtin, executable: './custom-storage', sha256: '" + digest + "'}", false},
		{"sqlite cannot claim process", "{backend: sqlite, execution: process}", false},
		{"default sqlite cannot claim process", "{execution: process}", false},
		{"explicit external worker", "{backend: plugin, execution: process, executable: './custom-storage', sha256: '" + digest + "'}", true},
		{"explicit builtin sqlite", "{backend: sqlite, execution: builtin}", true},
		{"official legacy sqlite migration", "{backend: plugin, execution: builtin, executable: './MusicFS-Plugin-Storage-SQLite.EXE', sha256: '" + digest + "'}", true},
		{"official bundle migration", "{backend: plugin, execution: builtin, executable: './musicfs-plugins', args: ['--plugin=storage-sqlite'], sha256: '" + digest + "'}", true},
		{"wrong bundle selector", "{backend: plugin, execution: builtin, executable: './musicfs-plugins', args: ['--plugin=netease'], sha256: '" + digest + "'}", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := minimalYAML + "plugins: []\nstorage: " + tc.settings + "\n"
			_, err := parse(t, raw)
			if (err == nil) != tc.valid {
				t.Fatalf("loader validity differs: %v", err)
			}
			var input any
			if err := yaml.Unmarshal([]byte(raw), &input); err != nil {
				t.Fatal(err)
			}
			body, _ := json.Marshal(input)
			if err := configurationSchema(t).Validate(testkit.DecodeJSON(t, body)); (err == nil) != tc.valid {
				t.Fatalf("schema validity differs: %v", err)
			}
		})
	}
}
