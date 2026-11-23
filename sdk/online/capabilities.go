package online

// CapabilityNames 是协议支持的方法集合；每个插件自行声明已实现的能力。
func CapabilityNames() []string {
	return []string{"search.tracks", "search.albums", "search.artists", "search.playlists", "metadata.album", "metadata.artist", "album.tracks", "artist.albums", "artist.tracks", "metadata.tracks.batch", "playlist.tracks", "metadata.track", "track.details", "playlist.observe", "playlist.list", "playlist.addTracks", "recommendations.playlists", "recommendations.tracks", "account.login", "stream.resolve"}
}

// DeclareCapabilities 生成完整声明，未启用的方法显式标记为不可用。
func DeclareCapabilities(enabled ...string) []Capability {
	set := map[string]bool{}
	for _, name := range enabled {
		set[name] = true
	}
	result := []Capability{}
	for _, name := range CapabilityNames() {
		reason := "Not implemented by this plugin"
		if set[name] {
			reason = "Implemented by this plugin"
		}
		result = append(result, Capability{Name: name, Supported: set[name], Reason: reason})
	}
	return result
}
