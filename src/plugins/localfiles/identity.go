package localfiles

import "github.com/fnMrRice/MusicFS-server/src/pluginproto"

// Identity declares the plugin-owned display names and user-facing features.
func Identity() *pluginproto.PluginIdentity {
	return &pluginproto.PluginIdentity{
		ID: "musicfs.localfiles", Name: "Local Music",
		LocalizedNames: map[string]string{
			"zh": "本地音乐", "zh-Hans": "本地音乐",
			"zh-Hant": "本機音樂", "ja": "ローカル音楽",
		},
		Features: []string{"music-source"},
	}
}
