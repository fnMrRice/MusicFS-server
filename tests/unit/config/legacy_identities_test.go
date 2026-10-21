package config_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/instanceid"
	"github.com/fnMrRice/MusicFS-server/src/core/config"
)

func TestLegacyConfigurationIdentitiesAndReferences(t *testing.T) {
	raw := `configVersion: 1
allowLoopbackHttp: true
allowedOrigins: ['http://127.0.0.1:8080']
plugins:
  - pluginId: musicfs.localfiles
    instances:
      - instanceId: local
        config: {}
  - pluginId: musicfs.scraper.local
    instances:
      - instanceId: scraper-local
        config: {}
metadata:
  automatic:
    instancePriority: ['scraper-local']
`
	doc, err := config.ParseDocument([]byte(raw), filepath.Join(t.TempDir(), "server.yaml"), map[string]string{"ADMIN_TOKEN": "configuration-test-token-long-enough"})
	if err != nil {
		t.Fatal(err)
	}
	if doc.Config.Plugins[0].PluginID != "musicfs.plugin.source.localfiles" || doc.Config.Plugins[0].Instances[0].InstanceID != instanceid.Canonical("local") || doc.Config.Plugins[0].Instances[0].Name != "" {
		t.Fatal("legacy plugin identity, UUID or empty alias was lost")
	}
	if doc.Config.Plugins[1].PluginID != "musicfs.scraper.local" || doc.Config.Metadata.Automatic.InstancePriority[0] != instanceid.Canonical("scraper-local") || string(doc.Raw) != raw {
		t.Fatal("scraper identity, references or original revision bytes changed")
	}
	duplicate := strings.Replace(raw, "metadata:", "  - pluginId: musicfs.plugin.source.localfiles\n    instances: []\nmetadata:", 1)
	if _, err := config.ParseDocument([]byte(duplicate), filepath.Join(t.TempDir(), "server.yaml"), map[string]string{"ADMIN_TOKEN": "configuration-test-token-long-enough"}); err == nil {
		t.Fatal("legacy and current plugin declarations were silently merged")
	}
}
