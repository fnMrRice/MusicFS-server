package config

// OnlinePlatform maps only the three fixed, trusted program identities. Their
// instance settings remain empty; no URL, cookie or token is accepted here.
func OnlinePlatform(pluginID string) string {
	switch pluginID {
	case "musicfs.plugin.source.netease":
		return "netease"
	case "musicfs.plugin.source.qqmusic":
		return "qqmusic"
	case "musicfs.plugin.source.applemusic":
		return "applemusic"
	case "musicfs.plugin.source.bilibili":
		return "bilibili"
	default:
		return ""
	}
}
