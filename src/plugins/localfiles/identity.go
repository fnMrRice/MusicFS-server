package localfiles

import "github.com/fnMrRice/MusicFS-server/src/pluginproto"

// Identity declares the plugin-owned display names and user-facing features.
func Identity() *pluginproto.PluginIdentity {
	return &pluginproto.PluginIdentity{
		ID: "musicfs.localfiles", Name: "Local Music Files",
		LocalizedNames: map[string]string{
			"zh": "本地音乐文件", "zh-Hans": "本地音乐文件",
			"zh-Hant": "本機音樂檔案", "ja": "ローカル音楽ファイル",
		},
		Features: []string{"music-source"},
	}
}
