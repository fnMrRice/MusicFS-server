package provideronline

import (
	"github.com/fnMrRice/MusicFS-server/src/online"
	"github.com/fnMrRice/MusicFS-server/src/pluginproto"
)

// Describe declares this plugin's identity, translations, and capabilities.
// These display values belong to the plugin, never the host or web client.
func Describe(platform string) pluginproto.OnlineDescriptor {
	descriptor := pluginproto.OnlinePolicy(platform)
	switch platform {
	case online.Netease:
		descriptor.Plugin = &pluginproto.OnlinePlugin{ID: "musicfs.netease", Name: "NetEase Cloud Music", LocalizedNames: map[string]string{"zh": "网易云音乐", "zh-Hans": "网易云音乐", "zh-Hant": "網易雲音樂"}}
	case online.QQMusic:
		descriptor.Plugin = &pluginproto.OnlinePlugin{ID: "musicfs.qqmusic", Name: "QQ Music", LocalizedNames: map[string]string{"zh": "QQ 音乐", "zh-Hans": "QQ 音乐", "zh-Hant": "QQ 音樂"}}
	case online.AppleMusic:
		descriptor.Plugin = &pluginproto.OnlinePlugin{ID: "musicfs.applemusic", Name: "Apple Music", LocalizedNames: map[string]string{}}
	}
	if descriptor.Plugin != nil {
		descriptor.Plugin.Features = []string{"music-source"}
	}
	return descriptor
}
