package localfiles

import (
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginconfig"
)

// Identity declares the plugin-owned display names and user-facing features.
func Identity() *pluginproto.PluginIdentity {
	return pluginconfig.Identity("musicfs.plugin.source.localfiles")
}
