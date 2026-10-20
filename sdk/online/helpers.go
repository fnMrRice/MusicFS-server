package online

import (
	"net/url"
	"regexp"
	"strings"
)

var numericID = regexp.MustCompile(`^[1-9][0-9]{0,19}$`)
var songMID = regexp.MustCompile(`^[A-Za-z0-9]{1,64}$`)

func SearchKind(kind string) bool {
	return kind == "tracks" || kind == "albums" || kind == "artists" || kind == "playlists"
}

// ImageURL normalizes an official Netease display image reference.
// These URLs must never become server-side fetch destinations.
func ImageURL(raw string) (*string, error) {
	if raw == "" {
		return nil, nil
	}
	u, err := url.Parse(raw)
	if err != nil || len(raw) > 2048 || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Port() != "" || u.RawQuery != "" || u.Fragment != "" || !strings.HasSuffix(u.Hostname(), ".music.126.net") {
		return nil, UpstreamInvalid()
	}
	u.Scheme = "https"
	value := u.String()
	return &value, nil
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
