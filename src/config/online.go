package config

// OnlinePlatform maps only the three fixed, trusted program identities. Their
// instance settings remain empty; no URL, cookie or token is accepted here.
func OnlinePlatform(pluginID string) string {
	switch pluginID {
	case "musicfs.netease":
		return "netease"
	case "musicfs.qqmusic":
		return "qqmusic"
	case "musicfs.applemusic":
		return "applemusic"
	default:
		return ""
	}
}
