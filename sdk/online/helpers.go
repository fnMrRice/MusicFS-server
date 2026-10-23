package online

func SearchKind(kind string) bool {
	return kind == "tracks" || kind == "albums" || kind == "artists" || kind == "playlists"
}

// ValidCount accepts an absent or non-negative metadata count.
func ValidCount(n *int64) bool { return n == nil || *n >= 0 }

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
