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

func TestLoggingDefaultsPathsAndPrecedence(t *testing.T) {
	doc, err := parse(t, minimalYAML+"plugins: []\n")
	if err != nil {
		t.Fatal(err)
	}
	if doc.Config.Logging.Level != "info" || doc.Config.Logging.Directory != filepath.Join(doc.Config.DataRoot, "logs") || len(doc.Config.Logging.Plugins) != 0 {
		t.Fatalf("unexpected logging defaults: %+v", doc.Config.Logging)
	}
	legacy := doc.Config
	legacy.Logging = config.LoggingConfig{}
	if err := legacy.Validate(); err != nil || !config.SameStartupSettings(legacy, doc.Config) {
		t.Fatalf("zero logging configuration did not preserve defaults: %v", err)
	}
	legacy.NormalizeRuntime()
	if legacy.Logging.Level != "info" || legacy.Logging.Directory != doc.Config.Logging.Directory {
		t.Fatal("runtime did not normalize zero logging defaults")
	}
	dir := t.TempDir()
	t.Setenv("MUSICFS_DATA_ROOT", "environment-data")
	t.Setenv("MUSICFS_LOG_LEVEL", "debug")
	path := filepath.Join(dir, "server.yaml")
	raw := []byte(minimalYAML + "plugins: []\nlogging: {level: warn, plugins: {musicfs.plugin.source.localfiles: trace}}\n")
	overrides := map[string]string{"ADMIN_TOKEN": "configuration-logging-test-credential"}
	doc, err = config.ParseDocument(raw, path, overrides)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Config.Logging.Level != "debug" || doc.Config.Logging.Directory != filepath.Join(dir, "environment-data", "logs") || doc.Config.Logging.Plugins["musicfs.plugin.source.localfiles"] != "trace" {
		t.Fatalf("logging environment precedence failed: %+v", doc.Config.Logging)
	}
	overrides["LOG_LEVEL"] = "error"
	doc, err = config.ParseDocument([]byte(minimalYAML+"plugins: []\nlogging: {directory: '../logs', level: info}\n"), path, overrides)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Config.Logging.Directory != filepath.Join(dir, "..", "logs") || doc.Config.Logging.Level != "error" {
		t.Fatal("explicit directory or override did not take precedence")
	}
	t.Setenv("MUSICFS_LOG_LEVEL", "")
	delete(overrides, "LOG_LEVEL")
	if _, err := config.ParseDocument(raw, path, overrides); err == nil {
		t.Fatal("empty logging environment override was accepted")
	}
}

func TestLoggingConfigurationLoaderAndSchema(t *testing.T) {
	schema := configurationSchema(t)
	for name, tc := range map[string]struct {
		value string
		valid bool
	}{
		"defaults":        {"{}", true},
		"trace":           {"{level: trace, plugins: {musicfs.scraper.local: debug}}", true},
		"unknown level":   {"{level: verbose}", false},
		"upper level":     {"{level: INFO}", false},
		"empty level":     {"{level: ''}", false},
		"empty directory": {"{directory: ''}", false},
		"blank directory": {"{directory: '  '}", false},
		"null":            {"null", false},
		"null plugins":    {"{plugins: null}", false},
		"null override":   {"{plugins: {musicfs.scraper.local: null}}", false},
		"wrong type":      {"{level: 10}", false},
		"path identifier": {"{plugins: {'../private': debug}}", false},
		"reserved name":   {"{plugins: {CON.log: debug}}", false},
		"trailing dot":    {"{plugins: {'plugin.': debug}}", false},
		"empty override":  {"{plugins: {musicfs.scraper.local: ''}}", false},
		"unknown option":  {"{retentionDays: 10}", false},
	} {
		t.Run(name, func(t *testing.T) {
			raw := minimalYAML + "plugins: []\nlogging: " + tc.value + "\n"
			_, err := parse(t, raw)
			if (err == nil) != tc.valid {
				t.Fatalf("loader validity=%v: %v", tc.valid, err)
			}
			var value any
			if err := yaml.Unmarshal([]byte(raw), &value); err != nil {
				t.Fatal(err)
			}
			body, _ := json.Marshal(value)
			if err := schema.Validate(testkit.DecodeJSON(t, body)); (err == nil) != tc.valid {
				t.Fatalf("schema validity=%v: %v", tc.valid, err)
			}
		})
	}
	for _, raw := range []string{
		minimalYAML + "logging: {level: info, level: debug}\n",
		minimalYAML + "logging: {plugins: {musicfs.scraper.local: debug, musicfs.scraper.local: trace}}\n",
		minimalYAML + "logging: {directory: '../中文 音乐/logs'}\n" + pluginYAML,
	} {
		if _, err := parse(t, raw); err == nil {
			t.Fatal("duplicate keys or overlapping music root was accepted")
		}
	}
}

func TestLoggingSnapshotIsolationAndRestartRequirement(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "server.yaml")
	raw := minimalYAML + "plugins: []\nlogging: {level: info, plugins: {musicfs.scraper.local: debug}}\n"
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	store, err := config.OpenStore(ctx, path, map[string]string{"ADMIN_TOKEN": "configuration-logging-test-credential"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []config.Config{store.Current(), store.Snapshot().Config} {
		c.Logging.Plugins["musicfs.scraper.local"] = "error"
	}
	if store.Current().Logging.Plugins["musicfs.scraper.local"] != "debug" {
		t.Fatal("logging snapshot map aliases active configuration")
	}
	encoded, _ := json.Marshal(store.Snapshot().Config.Logging)
	if strings.Contains(string(encoded), "directory") || strings.Contains(string(encoded), "logs") {
		t.Fatal("logging configuration projection exposes filesystem path")
	}
	if err := os.WriteFile(path, []byte(strings.Replace(raw, "level: info", "level: trace", 1)), 0600); err != nil {
		t.Fatal(err)
	}
	if err := store.Reload(ctx); err == nil || !strings.Contains(err.Error(), "restart is required") {
		t.Fatalf("logging deployment change reloaded without restart: %v", err)
	}
	if store.Current().Logging.Level != "info" {
		t.Fatal("failed reload modified active startup settings")
	}
}
