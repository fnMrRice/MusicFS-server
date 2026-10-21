package config_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/instanceid"
	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"go.yaml.in/yaml/v3"
)

const minimalYAML = "configVersion: 1\nallowLoopbackHttp: true\nallowedOrigins: ['http://127.0.0.1:8080']\n"
const pluginYAML = `plugins:
  - pluginId: musicfs.plugin.source.localfiles
    executable: '../workers/local files'
    enabled: true
    execution: process
    instances:
      - instanceId: b1548e96-3081-5c9c-8376-bf290e369d51
        enabled: true
        config:
          musicRoots:
            - id: root
              name: 中文音乐
              path: '../中文 音乐'
  - pluginId: musicfs.plugin.decoder.ffmpeg
    executable: '../workers/audio'
    enabled: true
    execution: process
    instances:
      - instanceId: 7edbaa88-7094-5657-ac43-37c118ef4f77
        enabled: true
        config:
          ffmpegPath: '../tools/ffmpeg'
          ffprobePath: '../tools/ffprobe'
  - pluginId: musicfs.plugin.decoder.ncm
    executable: '../workers/ncm'
    enabled: true
    execution: process
    instances:
      - instanceId: d1971de9-e153-57c6-ac53-82dfb631da0b
        enabled: true
        config: {}
`

func parse(t *testing.T, raw string) (*config.Document, error) {
	t.Helper()
	return config.ParseDocument([]byte(raw), filepath.Join(t.TempDir(), "配置.yaml"), map[string]string{"ADMIN_TOKEN": "test-credential-at-least-24-characters"})
}

func TestConfigPrecedencePathsRevisionAndRedaction(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "配置.yml")
	raw := []byte("# Keep this comment\n" + minimalYAML + pluginYAML)
	if err := os.WriteFile(file, raw, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MUSICFS_ADMIN_TOKEN", "secret-credential-long-enough")
	t.Setenv("MUSICFS_LISTEN", "127.0.0.1:9000")
	t.Setenv("MUSICFS_DATA_ROOT", "environment-data")
	t.Setenv("MUSICFS_WEB_ROOT", "../网页")
	t.Setenv("MUSICFS_PLUGIN_PATH", "untrusted-worker")
	t.Setenv("MUSICFS_FFMPEG_PATH", "untrusted-tool")
	t.Setenv("MUSICFS_ENABLED", "false")
	overrides := map[string]string{"LISTEN": "127.0.0.1:8000", "MEDIA_ROOT": "cli-media"}
	doc, err := config.ReadDocument(file, overrides)
	if err != nil {
		t.Fatal(err)
	}
	c := doc.Config
	if c.Listen != "127.0.0.1:8000" || c.DataRoot != filepath.Join(dir, "environment-data") || c.MediaRoot != filepath.Join(dir, "cli-media") || c.WebRoot != filepath.Join(dir, "..", "网页") {
		t.Fatal("precedence or path resolution failed")
	}
	if c.Plugins[0].Executable != filepath.Join(dir, "..", "workers", "local files") || c.Plugins[0].Instances[0].Config.MusicRoots[0].Path != filepath.Join(dir, "..", "中文 音乐") || c.Plugins[1].Instances[0].Config.FFmpegPath != filepath.Join(dir, "..", "tools", "ffmpeg") || !c.Plugins[0].Enabled {
		t.Fatal("plugin configuration changed through environment or path resolution")
	}
	if c.Plugins[0].Instances[0].Name != "" {
		t.Fatal("instance identity was used as a display name")
	}
	if doc.Path != file || !bytes.Equal(doc.Raw, raw) || doc.Revision != fmt.Sprintf("%x", sha256.Sum256(raw)) || doc.Root.Kind != yaml.DocumentNode {
		t.Fatal("document identity or raw bytes changed")
	}
	encoded, err := yaml.Marshal(doc.Root)
	if err != nil || !bytes.Contains(encoded, []byte("Keep this comment")) || bytes.Contains(encoded, []byte("cli-media")) || bytes.Contains(encoded, []byte("secret-credential")) {
		t.Fatal("AST lost comments or included effective overrides")
	}
	for _, marshal := range []func(any) ([]byte, error){json.Marshal, yaml.Marshal} {
		output, err := marshal(c)
		if err != nil || bytes.Contains(output, []byte("secret-credential")) || bytes.Contains(output, []byte("adminToken")) {
			t.Fatal("credential serialized into configuration")
		}
	}
	copyRaw := append([]byte(nil), raw...)
	parsed, err := config.ParseDocument(copyRaw, file, overrides)
	if err != nil {
		t.Fatal(err)
	}
	copyRaw[0] = '!'
	if parsed.Raw[0] != '#' {
		t.Fatal("document raw data aliases caller buffer")
	}
	changed, err := config.ParseDocument(append(raw, '\n'), file, overrides)
	if err != nil || changed.Revision == doc.Revision {
		t.Fatal("raw formatting edit did not change revision")
	}
	loaded, err := config.Load(file, overrides)
	if err != nil || loaded.Listen != c.Listen {
		t.Fatal("Load does not share document parser")
	}
}

func TestDisabledPluginsAndSingleLocalInstance(t *testing.T) {
	raw := minimalYAML + `plugins:
  - pluginId: musicfs.plugin.source.localfiles
    instances:
      - instanceId: first
        config: {}
  - pluginId: musicfs.plugin.decoder.ffmpeg
    executable: missing-worker
    instances:
      - instanceId: audio-one
        enabled: true
        config: {}
  - pluginId: musicfs.plugin.decoder.ncm
    instances:
      - instanceId: decoder
        config: {}
`
	doc, err := parse(t, raw)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Config.Plugins[0].Enabled || doc.Config.Plugins[0].Instances[0].Enabled {
		t.Fatal("enabled defaults or explicit false changed")
	}
	multi := strings.Replace(minimalYAML+pluginYAML, "  - pluginId: musicfs.plugin.decoder.ffmpeg", `      - instanceId: local-other
        enabled: true
        config:
          musicRoots:
            - id: root
              name: Other
              path: ../other-music
  - pluginId: musicfs.plugin.decoder.ffmpeg`, 1)
	if _, err := parse(t, multi); err == nil {
		t.Fatal("multiple local music instances were accepted")
	}
}

func TestConfigSemanticBoundaries(t *testing.T) {
	valid, err := parse(t, minimalYAML+pluginYAML)
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*config.Config){
		"version":          func(c *config.Config) { c.ConfigVersion = 2 },
		"public HTTP":      func(c *config.Config) { c.Listen = "0.0.0.0:8000" },
		"bad port":         func(c *config.Config) { c.Listen = "127.0.0.1:70000" },
		"weak credential":  func(c *config.Config) { c.AdminToken = "short" },
		"half TLS":         func(c *config.Config) { c.TLSCert = "certificate" },
		"origin query":     func(c *config.Config) { c.AllowedOrigins = []string{"http://127.0.0.1:8000?"} },
		"origin port":      func(c *config.Config) { c.AllowedOrigins = []string{"http://127.0.0.1:70000"} },
		"resource budget":  func(c *config.Config) { c.MaxCacheBytes = 1 },
		"writable overlap": func(c *config.Config) { c.DataRoot = c.CacheRoot },
		"music overlap":    func(c *config.Config) { c.Plugins[0].Instances[0].Config.MusicRoots[0].Path = c.CacheRoot },
		"root identity":    func(c *config.Config) { c.Plugins[0].Instances[0].Config.MusicRoots[0].ID = "../private" },
		"duplicate roots": func(c *config.Config) {
			roots := c.Plugins[0].Instances[0].Config.MusicRoots
			c.Plugins[0].Instances[0].Config.MusicRoots = append(roots, roots[0])
		},
		"duplicate plugins":   func(c *config.Config) { c.Plugins = append(c.Plugins, c.Plugins[0]) },
		"duplicate instances": func(c *config.Config) { c.Plugins[1].Instances[0].InstanceID = c.Plugins[0].Instances[0].InstanceID },
		"checksum":            func(c *config.Config) { c.Plugins[0].SHA256 = "not-a-checksum" },
		"missing executable":  func(c *config.Config) { c.Plugins[0].Executable = "" },
		"missing roots":       func(c *config.Config) { c.Plugins[0].Instances[0].Config.MusicRoots = nil },
		"missing tools":       func(c *config.Config) { c.Plugins[1].Instances[0].Config.FFprobePath = "" },
		"cross-role settings": func(c *config.Config) { c.Plugins[2].Instances[0].Config.FFmpegPath = "tool" },
		"multiple active audio": func(c *config.Config) {
			extra := c.Plugins[1].Instances[0]
			extra.InstanceID = instanceid.Canonical("audio-extra")
			c.Plugins[1].Instances = append(c.Plugins[1].Instances, extra)
		},
	} {
		t.Run(name, func(t *testing.T) {
			fresh, err := parse(t, minimalYAML+pluginYAML)
			if err != nil {
				t.Fatal(err)
			}
			mutate(&fresh.Config)
			if fresh.Config.Validate() == nil {
				t.Fatal("unsafe configuration accepted")
			}
		})
	}
	if valid.Config.Validate() != nil {
		t.Fatal("valid configuration rejected")
	}
}

func TestChecksumNormalizationPreservesDocument(t *testing.T) {
	hash := strings.Repeat("A1", 32)
	raw := strings.Replace(minimalYAML+pluginYAML, "    executable: '../workers/ncm'", "    executable: '../workers/ncm'\n    sha256: "+hash, 1)
	doc, err := parse(t, raw)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Config.Plugins[2].SHA256 != strings.ToLower(hash) || !bytes.Contains(doc.Raw, []byte(hash)) {
		t.Fatal("checksum was not normalized without changing source")
	}
}

func TestLoaderRejectsCaseCollidingInstanceIDs(t *testing.T) {
	t.Setenv("MUSICFS_ADMIN_TOKEN", "case-collision-test-credential-only")
	path := filepath.Join(t.TempDir(), "server.yaml")
	raw := minimalYAML + `plugins:
  - pluginId: musicfs.plugin.source.localfiles
    instances:
      - instanceId: Local
        config: {}
`
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.Load(path, nil)
	if err != nil || loaded.Plugins[0].Instances[0].InstanceID != instanceid.Canonical("Local") {
		t.Fatalf("legacy instance identity was not upgraded to a stable UUID: %v", err)
	}
	for name, suffix := range map[string]string{
		"same plugin": `      - instanceId: b1548e96-3081-5c9c-8376-bf290e369d51
        config: {}
`,
		"different plugin": `  - pluginId: musicfs.plugin.decoder.ncm
    instances:
      - instanceId: b1548e96-3081-5c9c-8376-bf290e369d51
        config: {}
`,
	} {
		t.Run(name, func(t *testing.T) {
			if err := os.WriteFile(path, []byte(raw+suffix), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := config.Load(path, nil); err == nil {
				t.Fatal("case-colliding instance IDs were accepted before startup")
			}
		})
	}
}
