package config

import "github.com/fnMrRice/MusicFS-server/src/plugins/pluginconfig"

// OnlinePlatform maps only the three fixed, trusted program identities. Their
// instance settings remain empty; no URL, cookie or token is accepted here.
func OnlinePlatform(pluginID string) string {
	d, _ := pluginconfig.Lookup(pluginID)
	return d.Platform
}
