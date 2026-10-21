// Package pluginid defines compatibility aliases for official plugin IDs.
package pluginid

// Canonical returns the current identity without interpreting unknown IDs.
func Canonical(id string) string {
	switch id {
	case "musicfs.localfiles":
		return "musicfs.plugin.source.localfiles"
	case "musicfs.netease":
		return "musicfs.plugin.source.netease"
	case "musicfs.qqmusic":
		return "musicfs.plugin.source.qqmusic"
	case "musicfs.applemusic":
		return "musicfs.plugin.source.applemusic"
	case "musicfs.audio.ffmpeg", "musicfs.audioffmpeg":
		return "musicfs.plugin.decoder.ffmpeg"
	case "musicfs.ncm":
		return "musicfs.plugin.decoder.ncm"
	default:
		return id
	}
}
