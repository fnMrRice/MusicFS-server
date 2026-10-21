package config_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/core/config"
)

func TestStrictYAMLRejectsUnsafeDocuments(t *testing.T) {
	cases := map[string]string{
		"missing version":       "allowLoopbackHttp: true\nallowedOrigins: ['http://127.0.0.1:8080']\n",
		"unknown version":       strings.Replace(minimalYAML, "configVersion: 1", "configVersion: 2", 1),
		"legacy field":          minimalYAML + "pluginPath: private-worker\n",
		"file credential":       minimalYAML + "adminToken: PRIVATE-SENTINEL\n",
		"unknown field":         minimalYAML + "PRIVATE-SENTINEL: hidden-value\n",
		"duplicate":             minimalYAML + "configVersion: 1\n",
		"multiple documents":    minimalYAML + "---\nconfigVersion: 1\n",
		"empty second document": minimalYAML + "---\n",
		"scalar document":       "PRIVATE-SENTINEL",
		"sequence document":     "[1, 2]",
		"empty document":        "# only a comment\n",
		"null document":         "null",
		"syntax":                minimalYAML + "plugins: [PRIVATE-SENTINEL\n",
		"scalar null":           minimalYAML + "dataRoot: null\n",
		"implicit null":         minimalYAML + "dataRoot:\n",
		"quoted bool":           strings.Replace(minimalYAML, "true", "'true'", 1),
		"legacy bool":           strings.Replace(minimalYAML, "true", "yes", 1),
		"noncanonical bool":     strings.Replace(minimalYAML, "true", "True", 1),
		"float int":             minimalYAML + "maxSessions: 8.0\n",
		"hex int":               minimalYAML + "maxSessions: 0x08\n",
		"overflow":              minimalYAML + "maxCacheBytes: 999999999999999999999999\n",
		"numeric string":        minimalYAML + "webRoot: 123\n",
		"timestamp string":      minimalYAML + "webRoot: 2026-10-09\n",
		"invalid path byte":     minimalYAML + "webRoot: \"private\\0file\"\n",
		"custom tag":            minimalYAML + "webRoot: !PRIVATE-SENTINEL private\n",
		"anchor":                minimalYAML + "webRoot: &private value\n",
		"alias":                 minimalYAML + "webRoot: *private\n",
		"merge":                 minimalYAML + "<<: {webRoot: private}\n",
		"numeric map key":       minimalYAML + "1: private\n",
		"enabled string":        strings.Replace(minimalYAML+pluginYAML, "enabled: true", "enabled: 'false'", 1),
		"enabled null":          strings.Replace(minimalYAML+pluginYAML, "enabled: true", "enabled: null", 1),
		"settings null":         strings.Replace(minimalYAML+pluginYAML, "config: {}", "config: null", 1),
		"wrong settings":        strings.Replace(minimalYAML+pluginYAML, "config: {}", "config: {ffmpegPath: private}", 1),
		"unknown nested":        strings.Replace(minimalYAML+pluginYAML, "ffmpegPath:", "PRIVATE-SENTINEL:", 1),
		"duplicate nested":      strings.Replace(minimalYAML+pluginYAML, "config: {}", "config: {}\n        enabled: true", 1),
		"configured role":       strings.Replace(minimalYAML+pluginYAML, "config: {}", "config: {}\n        role: audio", 1),
		"missing config":        strings.Replace(minimalYAML+pluginYAML, "        config: {}\n", "", 1),
		"missing instance ID":   strings.Replace(minimalYAML+pluginYAML, "      - instanceId: d1971de9-e153-57c6-ac53-82dfb631da0b\n", "      - name: NCM\n", 1),
		"unknown plugin":        strings.Replace(minimalYAML+pluginYAML, "musicfs.plugin.decoder.ncm", "PRIVATE-SENTINEL", 1),
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := parse(t, raw); err == nil {
				t.Fatal("invalid YAML accepted")
			} else if strings.Contains(err.Error(), "PRIVATE-SENTINEL") {
				t.Fatalf("configuration error disclosed input: %v", err)
			}
		})
	}
}

func TestYAMLBoundsExtensionsAndDiagnostics(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.yaml")
	credentials := map[string]string{"ADMIN_TOKEN": "test-credential-at-least-24-characters"}
	for _, name := range []string{"", "server.json", "server", "server.yaml.bak"} {
		if _, err := config.ParseDocument([]byte(minimalYAML), name, credentials); err == nil {
			t.Fatalf("invalid extension accepted: %s", name)
		}
	}
	if _, err := config.Load(filepath.Join(t.TempDir(), "absent.yaml"), credentials); err == nil {
		t.Fatal("missing file accepted")
	}
	for _, key := range []string{"PLUGIN_PATH", "FFMPEG_PATH", "NCM_WORKER_PATH", "ENABLED", "ADMIN_PRIVATE_VALUE"} {
		if _, err := config.ParseDocument([]byte(minimalYAML), path, map[string]string{"ADMIN_TOKEN": credentials["ADMIN_TOKEN"], key: "PRIVATE-SENTINEL"}); err == nil || strings.Contains(err.Error(), key) || strings.Contains(err.Error(), "PRIVATE-SENTINEL") {
			t.Fatalf("unsupported override accepted or leaked: %v", err)
		}
	}
	validSize := []byte(minimalYAML + "#" + strings.Repeat("x", config.MaxDocumentBytes-len(minimalYAML)-1))
	if _, err := config.ParseDocument(validSize, path, credentials); err != nil {
		t.Fatalf("valid size boundary rejected: %v", err)
	}
	tooLarge := append(validSize, 'x')
	if err := os.WriteFile(path, tooLarge, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := config.ReadDocument(path, credentials); err == nil {
		t.Fatal("oversized file accepted")
	}
	tooDeep := minimalYAML + "unknown: " + strings.Repeat("[", 33) + "1" + strings.Repeat("]", 33)
	if _, err := config.ParseDocument([]byte(tooDeep), path, credentials); err == nil || !strings.Contains(err.Error(), "32 levels") {
		t.Fatalf("deep YAML not rejected by depth limit: %v", err)
	}
	if _, err := parse(t, minimalYAML+"privateValue: secret\n"); err == nil || !strings.Contains(err.Error(), "line 4 column 1") || strings.Contains(err.Error(), "privateValue") {
		t.Fatalf("unsafe or missing field location: %v", err)
	}
	for _, value := range []string{`'C:\音乐\曲目 1'`, `'/music/中文 目录'`} {
		raw := strings.Replace(minimalYAML+pluginYAML, "'../中文 音乐'", value, 1)
		if _, err := parse(t, raw); err != nil {
			t.Fatalf("literal path rejected: %v", err)
		}
	}
	if _, err := config.ParseDocument(bytes.Repeat([]byte{0xff}, 30), path, credentials); err == nil {
		t.Fatal("invalid UTF-8 accepted")
	}
}
