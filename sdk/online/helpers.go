package online

func SearchKind(kind string) bool {
	return kind == "tracks" || kind == "albums" || kind == "artists" || kind == "playlists"
}

// ValidCount accepts an absent or non-negative metadata count.
func ValidCount(n *int64) bool { return n == nil || *n >= 0 }

// TrackDigest 忽略展示用封面地址，避免 CDN 路由变化触发曲目重新发布。
func TrackDigest(track Track) string {
	track.MetadataDigest = ""
	track.CoverURL = nil
	return Digest(track)
}

// PlaylistDigest binds an ordered observation to display metadata while
// excluding CDN routing changes from playlist membership identity.
func PlaylistDigest(playlist Playlist, entries []PlaylistEntry) string {
	// CDN routing is not a change to the observed playlist membership.
	playlist.CoverURL = nil
	return Digest(struct {
		Playlist Playlist
		Entries  []PlaylistEntry
	}{playlist, entries})
}
