package pluginconfig_test

import (
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginconfig"
)

func TestOfficialManifestInstancePolicy(t *testing.T) {
	for pluginID, multiple := range map[string]bool{
		"musicfs.plugin.source.localfiles": false,
		"musicfs.plugin.source.netease":    true,
		"musicfs.plugin.source.qqmusic":    true,
		"musicfs.plugin.source.applemusic": true,
		"musicfs.plugin.decoder.ffmpeg":    false,
		"musicfs.plugin.decoder.ncm":       false,
		"musicfs.scraper.local":            true,
	} {
		t.Run(pluginID, func(t *testing.T) {
			role := pluginconfig.Role(pluginID)
			manifest := pluginproto.Manifest{
				PluginID: pluginID, Plugin: pluginconfig.Identity(pluginID),
				SupportsMultipleInstances: multiple, PluginVersion: "1.0.0", ProtocolVersion: pluginproto.ProtocolVersion,
				Roles: []string{role}, MaxMessageBytes: pluginproto.MaxMessageBytes, MaxPageItems: pluginproto.MaxPageItems,
			}
			if err := pluginconfig.ValidateManifest(manifest, role); err != nil {
				t.Fatal(err)
			}
			manifest.SupportsMultipleInstances = !multiple
			if pluginconfig.ValidateManifest(manifest, role) == nil {
				t.Fatal("host accepted contradictory instance support")
			}
			manifest.Plugin.Name = "Caller mutation"
			if pluginconfig.Identity(pluginID).Name == "Caller mutation" {
				t.Fatal("plugin identity aliases shared defaults")
			}
		})
	}
	if pluginconfig.SupportsMultipleInstances("unknown") || (pluginproto.Manifest{}).SupportsMultipleInstances {
		t.Fatal("undeclared plugins must default to one instance")
	}
}
