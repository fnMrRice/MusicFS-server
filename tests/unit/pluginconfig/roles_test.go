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
		"musicfs.plugin.source.bilibili":   true,
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
			if role == "provider" {
				manifest.SourceType = pluginproto.SourceNetwork
				if pluginID == "musicfs.plugin.source.localfiles" {
					manifest.SourceType = pluginproto.SourceLocal
				}
			}
			if role == "audio" && (len(manifest.Plugin.Features) != 1 || manifest.Plugin.Features[0] != "audio-codec") {
				t.Fatal("official audio plugin is not classified as an audio codec", manifest.Plugin)
			}
			if pluginID == "musicfs.scraper.local" && (role != "scraper" || len(manifest.Plugin.Features) != 1 || manifest.Plugin.Features[0] != "metadata") {
				t.Fatal("metadata classification changed the scraper role or retained legacy features", manifest)
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
