package config_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
	"go.yaml.in/yaml/v3"
)

func TestExtensionConfigurationAndEditorSchema(t *testing.T) {
	digest := strings.Repeat("a", 64)
	external := "storage: {backend: plugin, executable: './storage-worker', sha256: '" + digest + "'}\n"
	dsp := "audioDSP:\n  enabled: true\n  chain: [{name: gain, version: 1, params: {gainDB: -3.5}}]\n"
	for name, tc := range map[string]struct {
		raw string
		ok  bool
	}{
		"default sqlite":         {minimalYAML + "storage: {backend: sqlite}\n", true},
		"cannot disable storage": {minimalYAML + "storage: {backend: sqlite, enabled: false}\n", false},
		"external storage":       {minimalYAML + external, true},
		"builtin gain DSP":       {minimalYAML + "audioDSP: {plugins: [{name: musicfs.dsp.gain, builtin: true}]}\n", true},
		"unknown builtin DSP":    {minimalYAML + "audioDSP: {plugins: [{name: custom.dsp, builtin: true, executable: './dsp', sha256: '" + digest + "'}]}\n", false},
		"explicit external DSP":  {minimalYAML + "audioDSP: {plugins: [{name: custom.dsp, builtin: false, executable: './dsp', sha256: '" + digest + "'}]}\n", true},
		"ordered DSP":            {minimalYAML + pluginYAML + dsp, true},
		"missing hash":           {minimalYAML + "storage: {backend: plugin, executable: worker}\n", false},
		"driver executable":      {minimalYAML + "storage: {backend: sqlite, executable: worker}\n", false},
		"unknown field":          {minimalYAML + "storage: {backend: sqlite, password: secret}\n", false},
		"missing version":        {minimalYAML + pluginYAML + "audioDSP: {chain: [{name: gain}]}\n", false},
		"wrong version":          {minimalYAML + pluginYAML + strings.Replace(dsp, "version: 1", "version: 2", 1), false},
		"string parameter":       {minimalYAML + pluginYAML + strings.Replace(dsp, "-3.5", "'-3.5'", 1), false},
		"unknown DSP field":      {minimalYAML + pluginYAML + strings.Replace(dsp, "params:", "parameters:", 1), false},
	} {
		t.Run(name, func(t *testing.T) {
			doc, err := parse(t, tc.raw)
			if (err == nil) != tc.ok {
				t.Fatalf("loader validity differs: %v", err)
			}
			var v any
			if err := yaml.Unmarshal([]byte(tc.raw), &v); err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(v)
			if err != nil {
				t.Fatal(err)
			}
			err = configurationSchema(t).Validate(testkit.DecodeJSON(t, raw))
			if (err == nil) != tc.ok {
				t.Fatalf("schema validity differs: %v", err)
			}
			if tc.ok && strings.Contains(tc.raw, "backend: plugin") && !filepath.IsAbs(doc.Config.Storage.Executable) {
				t.Fatal("storage executable was not resolved against configuration directory")
			}
		})
	}
	for _, number := range []string{".nan", ".inf", "-.inf", "1e1000"} {
		if _, err := parse(t, minimalYAML+pluginYAML+strings.Replace(dsp, "-3.5", number, 1)); err == nil {
			t.Fatal("non-finite DSP parameter accepted")
		}
	}
	if _, err := parse(t, minimalYAML+dsp); err == nil {
		t.Fatal("required DSP accepted without an enabled audio processor")
	}
}

func TestStorageSnapshotRedactionAndCopyIsolation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.yaml")
	raw := minimalYAML + "storage:\n  backend: plugin\n  executable: './worker'\n  sha256: '" +
		strings.Repeat("a", 64) + "'\n  args: ['PRIVATE-ARG']\n  options: {connection: 'PRIVATE-CONNECTION'}\n"
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	store, err := config.OpenStore(context.Background(), path, map[string]string{"ADMIN_TOKEN": "test-credential-at-least-24-characters"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	current := store.Current()
	current.Storage.Options["connection"] = "changed"
	current.Storage.Args[0] = "changed"
	if store.Current().Storage.Options["connection"] != "PRIVATE-CONNECTION" || store.Current().Storage.Args[0] != "PRIVATE-ARG" {
		t.Fatal("storage configuration shares mutable data")
	}
	snapshot := store.Snapshot()
	if len(snapshot.Config.Storage.Options) != 0 || len(snapshot.Config.Storage.Args) != 0 || snapshot.Config.Storage.Executable != "" {
		t.Fatal("storage settings exposed in snapshot")
	}
	body, err := json.Marshal(snapshot)
	if err != nil || strings.Contains(string(body), "PRIVATE-") {
		t.Fatal("private storage configuration exposed in JSON")
	}
}

func TestStartupSettingsPreserveExplicitEmptyCollections(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.yaml")
	digest := strings.Repeat("a", 64)
	raw := minimalYAML + "storage: {backend: plugin, executable: './storage', sha256: '" + digest + "', args: [], options: {}}\n" +
		"audioDSP: {chain: [], plugins: [{name: example, executable: './dsp', sha256: '" + digest + "', args: []}]}\n"
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	overrides := map[string]string{"ADMIN_TOKEN": "test-credential-at-least-24-characters"}
	doc, err := config.ReadDocument(path, overrides)
	if err != nil {
		t.Fatal(err)
	}
	store, err := config.OpenStoreForStartup(context.Background(), path, overrides, nil, doc.Config)
	if err != nil {
		t.Fatal(err)
	}
	if !config.SameStartupSettings(doc.Config, store.Current()) {
		t.Fatal("cloning empty deployment arguments requires an unnecessary restart")
	}
}

func TestDeploymentChangesRequireRestartBeforeDatabaseReads(t *testing.T) {
	for name, change := range map[string]string{
		"storage":  "storage: {backend: plugin, executable: './must-not-run', sha256: '" + strings.Repeat("a", 64) + "'}\n",
		"roots":    "dataRoot: './new-data'\n",
		"dsp":      "audioDSP: {chain: [{name: bypass, version: 1}]}\n",
		"channels": "audioChannels: {outputLayout: '7.1'}\n",
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "server.yaml")
			original := minimalYAML + pluginYAML
			if err := os.WriteFile(path, []byte(original), 0600); err != nil {
				t.Fatal(err)
			}
			overrides := map[string]string{"ADMIN_TOKEN": "test-credential-at-least-24-characters"}
			store, err := config.OpenStore(context.Background(), path, overrides, nil)
			if err != nil {
				t.Fatal(err)
			}
			before := store.Current()
			if err = os.WriteFile(path, []byte(original+change), 0600); err != nil {
				t.Fatal(err)
			}
			if err = store.Reload(context.Background()); err == nil || !strings.Contains(err.Error(), "restart") {
				t.Fatalf("deployment change was not rejected before reading state: %v", err)
			}
			if !config.SameStartupSettings(before, store.Current()) {
				t.Fatal("failed reload changed runtime deployment")
			}
			if _, err = config.OpenStoreForStartup(context.Background(), path, overrides, nil, before); err == nil {
				t.Fatal("startup drift accepted")
			}
		})
	}
}
