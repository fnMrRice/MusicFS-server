package localfiles

import (
	"github.com/fnMrRice/MusicFS-server/src/pluginconfig"
	"github.com/fnMrRice/MusicFS-server/src/pluginproto"
)

// Identity declares the plugin-owned display names and user-facing features.
func Identity() *pluginproto.PluginIdentity {
	return pluginconfig.Identity("musicfs.plugin.source.localfiles")
}
