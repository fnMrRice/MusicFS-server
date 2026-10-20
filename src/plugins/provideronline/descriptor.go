package provideronline

import (
	"github.com/fnMrRice/MusicFS-server/src/pluginconfig"
	"github.com/fnMrRice/MusicFS-server/src/pluginproto"
)

// Describe declares this plugin's identity, translations, and capabilities.
// These display values belong to the plugin, never the host or web client.
func Describe(platform string) pluginproto.OnlineDescriptor {
	descriptor := pluginproto.OnlinePolicy(platform)
	descriptor.Plugin = pluginconfig.Identity(pluginproto.OnlinePluginID(platform))
	return descriptor
}
