package bootstrap

import (
	onlineplugins "github.com/fnMrRice/MusicFS-server/plugins/source/online"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginsettings"
	"github.com/fnMrRice/MusicFS-server/src/playback/onlinecache"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginconfig"
	"github.com/fnMrRice/MusicFS-server/src/plugins/settings"
)

func onlinePlaybackSettings() []settings.Provider {
	var providers []settings.Provider
	for _, descriptor := range onlineplugins.Descriptors() {
		id := descriptor.Plugin.ID
		if !pluginconfig.HasSettings(id) {
			continue
		}
		plugin := onlineplugins.Plugin(descriptor.Platform, onlineplugins.Options{})
		fields := append(plugin.PlaybackSettings, onlinecache.SettingsFields()...)
		providers = append(providers, settings.Provider{ID: id, Validate: onlinecache.ValidateSettings, Schema: pluginsettings.Schema{
			Revision: "online-playback-v1", Title: "Online playback",
			LocalizedTitles: map[string]string{"zh": "在线播放", "zh-Hant": "線上播放", "ja": "オンライン再生"},
			Fields:          fields, Actions: []pluginsettings.Action{},
		}})
	}
	return providers
}
