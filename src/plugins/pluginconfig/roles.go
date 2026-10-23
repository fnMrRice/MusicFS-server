package pluginconfig

import "github.com/fnMrRice/MusicFS-server/sdk/pluginproto"

// Role is the official plugin declaration shared by workers and the host.
// Hosts still validate the worker's manifest before admitting runtime work.
func Role(pluginID string) string {
	switch pluginID {
	case "musicfs.plugin.source.localfiles", "musicfs.plugin.source.netease", "musicfs.plugin.source.qqmusic", "musicfs.plugin.source.applemusic", "musicfs.plugin.source.bilibili":
		return "provider"
	case "musicfs.plugin.decoder.ffmpeg", "musicfs.plugin.decoder.ncm":
		return "audio"
	case "musicfs.scraper.local":
		return "scraper"
	default:
		return ""
	}
}

// SupportsMultipleInstances declares independent configuration support.
// Unknown SDK plugins default to a single instance until explicitly declared.
func SupportsMultipleInstances(pluginID string) bool {
	switch pluginID {
	case "musicfs.plugin.source.netease", "musicfs.plugin.source.qqmusic", "musicfs.plugin.source.applemusic", "musicfs.plugin.source.bilibili", "musicfs.scraper.local":
		return true
	default:
		return false
	}
}

// ValidateManifest checks the worker declaration against the official policy.
func ValidateManifest(manifest pluginproto.Manifest, role string) error {
	if err := manifest.Validate(role); err != nil {
		return err
	}
	if Role(manifest.PluginID) != role || manifest.SupportsMultipleInstances != SupportsMultipleInstances(manifest.PluginID) {
		return pluginproto.Failure("PROTOCOL_MISMATCH", "Plugin instance support does not match its declaration")
	}
	return nil
}
