package online

import (
	"strings"

	"github.com/fnMrRice/MusicFS-server/sdk/fault"
)

// The host validates worker results independently before publishing them.
func ValidateTrack(policy ResourcePolicy, track Track) error {
	wantURL := ReferenceURL(policy.Tracks, track.ExternalID)
	if wantURL == "" || track.SourceURL != wantURL || !validImageReference(policy, track.CoverURL) || track.PlaybackAvailability != "unknown" || track.Artists == nil || len(track.Artists) > 128 {
		return invalidResult()
	}
	title, err := TextValue(track.Title, 4096)
	if err != nil || title == "" || title != track.Title {
		return invalidResult()
	}
	if album, err := TextValue(track.Album, 4096); err != nil || album != track.Album {
		return invalidResult()
	}
	for _, artist := range track.Artists {
		if s, err := TextValue(artist, 4096); err != nil || s == "" || s != artist {
			return invalidResult()
		}
	}
	if track.DurationMs != nil && (*track.DurationMs <= 0 || *track.DurationMs > 7*24*60*60*1000) {
		return invalidResult()
	}
	if track.MetadataDigest != TrackDigest(track) {
		return invalidResult()
	}
	return nil
}

func ValidateSearch(policy ResourcePolicy, page SearchPage, limit int) error {
	n := len(page.Tracks) + len(page.Albums) + len(page.Artists) + len(page.Playlists)
	if limit < 1 || limit > MaxPageItems || !SearchKind(page.Kind) || page.Tracks == nil || page.Albums == nil || page.Artists == nil || page.Playlists == nil || n > limit || page.Consistency != Unversioned || page.ObservedAt.IsZero() || (page.Truncated && page.NextCursor != nil) {
		return invalidResult()
	}
	if page.NextCursor != nil && (len(*page.NextCursor) < 1 || len(*page.NextCursor) > 1024 || n == 0 || strings.ContainsAny(*page.NextCursor, " \r\n\t")) {
		return invalidResult()
	}
	for _, track := range page.Tracks {
		if page.Kind != "tracks" {
			return invalidResult()
		}
		if err := ValidateTrack(policy, track); err != nil {
			return err
		}
	}
	for _, album := range page.Albums {
		if page.Kind != "albums" || ValidateAlbum(policy, album) != nil {
			return invalidResult()
		}
	}
	for _, artist := range page.Artists {
		if page.Kind != "artists" || ValidateArtist(policy, artist) != nil {
			return invalidResult()
		}
	}
	for _, playlist := range page.Playlists {
		if page.Kind != "playlists" || validatePlaylist(policy, playlist) != nil {
			return invalidResult()
		}
	}
	return nil
}

func validatePlaylist(policy ResourcePolicy, p Playlist) error {
	wantURL := ReferenceURL(policy.Playlists, p.ExternalID)
	if wantURL == "" || p.SourceURL != wantURL || (p.TrackCount != nil && *p.TrackCount < 0) {
		return invalidResult()
	}
	if title, err := TextValue(p.Title, 4096); err != nil || title == "" || title != p.Title {
		return invalidResult()
	}
	if !validImageReference(policy, p.CoverURL) || p.Creator != nil && !validText(*p.Creator) {
		return invalidResult()
	}
	return nil
}

func ValidateRecommendations(policy ResourcePolicy, page RecommendationPage, limit int) error {
	if limit < 1 || limit > MaxPageItems || page.Items == nil || len(page.Items) > limit || page.Personalized || page.NextCursor != nil || page.ObservedAt.IsZero() || page.ExpiresAt.Sub(page.ObservedAt).Seconds() != 120 {
		return invalidResult()
	}
	for i, item := range page.Items {
		if item.Position != i || validatePlaylist(policy, item.Playlist) != nil {
			return invalidResult()
		}
		if reason, err := TextValue(item.Reason, 4096); err != nil || reason != item.Reason {
			return invalidResult()
		}
	}
	return nil
}

func ValidateObservation(policy ResourcePolicy, p PlaylistObservation, id string) error {
	validCount := p.Playlist.TrackCount != nil && *p.Playlist.TrackCount == int64(len(p.Entries))
	if validatePlaylist(policy, p.Playlist) != nil || p.Playlist.ExternalID != id || p.Entries == nil || len(p.Entries) > MaxPlaylistEntries || p.ObservedAt.IsZero() || p.Consistency != Unversioned || (p.Complete && !validCount) || (!p.Complete && validCount) {
		return invalidResult()
	}
	for i, entry := range p.Entries {
		validID := policy.ValidPlaylistEntry(entry.ExternalTrackID)
		if entry.Position != i || !validID {
			return invalidResult()
		}
	}
	if p.MetadataDigest != PlaylistDigest(p.Playlist, p.Entries) {
		return invalidResult()
	}
	return nil
}

func invalidResult() error {
	return fault.Fail("PROTOCOL_MISMATCH", "Online worker result is invalid")
}

func ValidatePlaylists(policy ResourcePolicy, page PlaylistsPage, limit int) error {
	if limit < 1 || limit > MaxPageItems || page.Items == nil || len(page.Items) > limit || page.Consistency != Unversioned || page.ObservedAt.IsZero() {
		return invalidResult()
	}
	if page.NextCursor != nil && (len(*page.NextCursor) < 1 || len(*page.NextCursor) > 1024 || len(page.Items) == 0 || strings.ContainsAny(*page.NextCursor, " \r\n\t")) {
		return invalidResult()
	}
	seen := map[string]bool{}
	for _, playlist := range page.Items {
		if validatePlaylist(policy, playlist) != nil || seen[playlist.ExternalID] {
			return invalidResult()
		}
		seen[playlist.ExternalID] = true
	}
	return nil
}
