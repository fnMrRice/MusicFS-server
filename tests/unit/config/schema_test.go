package config_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"go.yaml.in/yaml/v3"
)

func configurationSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	schemaPath := filepath.Join(testkit.RepositoryRoot(), "configs", "server.schema.json")
	content, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatal(err)
	}
	compiler := jsonschema.NewCompiler()
	url := "https://musicfs.invalid/configs/server.schema.json"
	if err := compiler.AddResource(url, testkit.DecodeJSON(t, content)); err != nil {
		t.Fatal(err)
	}
	schema, err := compiler.Compile(url)
	if err != nil {
		t.Fatal(err)
	}
	return schema
}

func TestConfigurationSchema(t *testing.T) {
	schema := configurationSchema(t)
	for name, tc := range map[string]struct {
		raw   string
		valid bool
	}{
		"minimal":                     {minimalYAML, true},
		"plugins":                     {minimalYAML + pluginYAML, true},
		"unlimited recursion":         {strings.Replace(minimalYAML+pluginYAML, "importDefaults: {importLyrics: false}", "importDefaults: {importLyrics: false, recursive: true, maxDepth: 0}", 1), true},
		"flat recursion":              {strings.Replace(minimalYAML+pluginYAML, "importDefaults: {importLyrics: false}", "importDefaults: {importLyrics: false, recursive: false, maxDepth: 0}", 1), true},
		"contradictory recursion":     {strings.Replace(minimalYAML+pluginYAML, "importDefaults: {importLyrics: false}", "importDefaults: {importLyrics: false, recursive: false, maxDepth: 1}", 1), false},
		"unknown top":                 {minimalYAML + "pluginPath: legacy", false},
		"secret":                      {minimalYAML + "adminToken: private", false},
		"version":                     {strings.Replace(minimalYAML, "Version: 1", "Version: 2", 1), false},
		"null":                        {minimalYAML + "webRoot: null", false},
		"enabled string":              {strings.Replace(minimalYAML+pluginYAML, "enabled: true", "enabled: 'false'", 1), false},
		"ncm config":                  {strings.Replace(minimalYAML+pluginYAML, "config: {}", "config: {musicRoots: []}", 1), false},
		"configured role":             {strings.Replace(minimalYAML+pluginYAML, "instanceId: b1548e96-3081-5c9c-8376-bf290e369d51\n", "instanceId: b1548e96-3081-5c9c-8376-bf290e369d51\n        role: provider\n", 1), false},
		"missing dependency":          {strings.Replace(minimalYAML+pluginYAML, "ffprobePath: '../tools/ffprobe'", "ffprobePath: ''", 1), false},
		"disabled missing dependency": {strings.ReplaceAll(strings.Replace(minimalYAML+pluginYAML, "ffprobePath: '../tools/ffprobe'", "ffprobePath: ''", 1), "enabled: true", "enabled: false"), true},
	} {
		t.Run(name, func(t *testing.T) {
			var value any
			if err := yaml.Unmarshal([]byte(tc.raw), &value); err != nil {
				t.Fatal(err)
			}
			body, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			err = schema.Validate(testkit.DecodeJSON(t, body))
			if (err == nil) != tc.valid {
				t.Fatalf("unexpected schema validity: %v", err)
			}
		})
	}
}

func TestOfficialYAMLExampleMatchesLoaderAndSchema(t *testing.T) {
	for _, key := range []string{"LISTEN", "DATA_ROOT", "MEDIA_ROOT", "CACHE_ROOT", "WEB_ROOT"} {
		name := "MUSICFS_" + key
		// Setenv registers restoration; remove the override rather than applying an empty value.
		t.Setenv(name, "")
		if err := os.Unsetenv(name); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("MUSICFS_ADMIN_TOKEN", "official-example-test-credential-only")
	path := filepath.Join(testkit.RepositoryRoot(), "configs", "server.example.yaml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := config.Load(path, nil)
	if err != nil {
		t.Fatalf("official example failed the file loader: %v", err)
	}
	document, err := config.ParseDocument(raw, path, nil)
	if err != nil {
		t.Fatalf("official example failed the document parser: %v", err)
	}
	if !reflect.DeepEqual(loaded, document.Config) {
		t.Fatal("official example has different file and document configurations")
	}
	var value any
	if err := document.Root.Decode(&value); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := configurationSchema(t).Validate(testkit.DecodeJSON(t, body)); err != nil {
		t.Fatalf("official example failed the configuration schema: %v", err)
	}
}

func TestLocalPendingConfigurationMatchesLoaderAndSchema(t *testing.T) {
	for _, permission := range []string{"", "        permissions: {readDirectories: []}\n", "        permissions: {readDirectories: ['./music']}\n"} {
		raw := minimalYAML + `pluginBundle: ''
plugins:
  - pluginId: musicfs.plugin.source.localfiles
    executable: './local-worker'
    enabled: true
    instances:
      - instanceId: b1548e96-3081-5c9c-8376-bf290e369d51
        enabled: true
        config: {}
` + permission
		doc, err := parse(t, raw)
		if err != nil {
			t.Fatal(err)
		}
		if !doc.Config.Plugins[0].Instances[0].PendingConfiguration {
			t.Fatal("unselected local instance should remain pending")
		}
		var value any
		if err := yaml.Unmarshal([]byte(raw), &value); err != nil {
			t.Fatal(err)
		}
		body, _ := json.Marshal(value)
		if err := configurationSchema(t).Validate(testkit.DecodeJSON(t, body)); err != nil {
			t.Fatalf("pending local configuration rejected by editor schema: %v", err)
		}
	}
}
