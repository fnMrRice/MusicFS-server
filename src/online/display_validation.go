package online

import "encoding/hex"

func validText(value string) bool {
	n, err := textValue(value, 4096)
	return err == nil && n != "" && n == value
}
func validImageReference(value *string) bool {
	if value == nil {
		return true
	}
	n, err := imageURL(*value)
	return err == nil && n != nil && *n == *value
}
func ValidateAlbum(platform string, a Album) error {
	if platform != Netease || !numericID.MatchString(a.ExternalID) || !validText(a.Title) || a.SourceURL != "https://music.163.com/album?id="+a.ExternalID || !validCount(a.TrackCount) || !validImageReference(a.CoverURL) || a.Artists == nil || len(a.Artists) > 128 {
		return invalidResult()
	}
	for _, name := range a.Artists {
		if !validText(name) {
			return invalidResult()
		}
	}
	return nil
}
func ValidateArtist(platform string, a Artist) error {
	if platform != Netease || !numericID.MatchString(a.ExternalID) || !validText(a.Name) || a.SourceURL != "https://music.163.com/artist?id="+a.ExternalID || !validCount(a.AlbumCount) || !validCount(a.TrackCount) || !validImageReference(a.ImageURL) {
		return invalidResult()
	}
	return nil
}
func ValidateTrackBatch(platform string, b TrackBatch, ids []string) error {
	if len(ids) < 1 || len(ids) > MaxPageItems || len(b.Items) != len(ids) {
		return invalidResult()
	}
	for i, item := range b.Items {
		if item.ExternalID != ids[i] {
			return invalidResult()
		}
		if item.Track == nil {
			if item.ErrorCode == nil || *item.ErrorCode != "NOT_FOUND" {
				return invalidResult()
			}
		} else if item.ErrorCode != nil || item.Track.ExternalID != item.ExternalID || ValidateTrack(platform, *item.Track) != nil {
			return invalidResult()
		}
	}
	return nil
}
func ValidatePlaylistTracks(platform string, p PlaylistTracksPage, r PlaylistTracksRequest) error {
	digest, err := hex.DecodeString(p.MetadataDigest)
	if err != nil || len(digest) != 32 || validatePlaylist(platform, p.Playlist) != nil || p.Playlist.ExternalID != r.ExternalID || p.Items == nil || len(p.Items) > r.Limit || p.ObservedAt.IsZero() || p.Consistency != Unversioned {
		return invalidResult()
	}
	if p.NextCursor != nil && (len(*p.NextCursor) < 1 || len(*p.NextCursor) > 1024 || len(p.Items) == 0) {
		return invalidResult()
	}
	for i, item := range p.Items {
		if item.Position < 0 || item.Position >= MaxPlaylistEntries || (i > 0 && item.Position != p.Items[i-1].Position+1) || (r.Cursor == "" && i == 0 && item.Position != 0) {
			return invalidResult()
		}
		if err := ValidateTrackBatch(platform, TrackBatch{Items: []TrackLookup{item.TrackLookup}}, []string{item.ExternalID}); err != nil {
			return err
		}
	}
	return nil
}
