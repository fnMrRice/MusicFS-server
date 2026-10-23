package online

import "encoding/hex"

func validText(value string) bool {
	n, err := TextValue(value, 4096)
	return err == nil && n != "" && n == value
}
func validImageReference(policy ResourcePolicy, value *string) bool {
	if value == nil {
		return true
	}
	n, err := policy.ImageURL(*value)
	return err == nil && n != nil && *n == *value
}
func ValidateAlbum(policy ResourcePolicy, a Album) error {
	if ReferenceURL(policy.Albums, a.ExternalID) == "" || !validText(a.Title) || a.SourceURL != ReferenceURL(policy.Albums, a.ExternalID) || !ValidCount(a.TrackCount) || !validImageReference(policy, a.CoverURL) || a.Artists == nil || len(a.Artists) > 128 {
		return invalidResult()
	}
	for _, name := range a.Artists {
		if !validText(name) {
			return invalidResult()
		}
	}
	return nil
}
func ValidateArtist(policy ResourcePolicy, a Artist) error {
	if ReferenceURL(policy.Artists, a.ExternalID) == "" || !validText(a.Name) || a.SourceURL != ReferenceURL(policy.Artists, a.ExternalID) || !ValidCount(a.AlbumCount) || !ValidCount(a.TrackCount) || !validImageReference(policy, a.ImageURL) {
		return invalidResult()
	}
	return nil
}
func ValidateTrackBatch(policy ResourcePolicy, b TrackBatch, ids []string) error {
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
		} else if item.ErrorCode != nil || item.Track.ExternalID != item.ExternalID || ValidateTrack(policy, *item.Track) != nil {
			return invalidResult()
		}
	}
	return nil
}
func ValidatePlaylistTracks(policy ResourcePolicy, p PlaylistTracksPage, r PlaylistTracksRequest) error {
	decodedDigest, err := hex.DecodeString(p.MetadataDigest)
	if err != nil || len(decodedDigest) != 32 || validatePlaylist(policy, p.Playlist) != nil || p.Playlist.ExternalID != r.ExternalID || p.Items == nil || len(p.Items) > r.Limit || p.ObservedAt.IsZero() || p.Consistency != Unversioned {
		return invalidResult()
	}
	if p.NextCursor != nil && (len(*p.NextCursor) < 1 || len(*p.NextCursor) > 1024 || len(p.Items) == 0) {
		return invalidResult()
	}
	for i, item := range p.Items {
		if !policy.ValidPlaylistEntry(item.ExternalID) || item.Position < 0 || item.Position >= MaxPlaylistEntries || (i > 0 && item.Position != p.Items[i-1].Position+1) || (r.Cursor == "" && i == 0 && item.Position != 0) {
			return invalidResult()
		}
		if err := ValidateTrackBatch(policy, TrackBatch{Items: []TrackLookup{item.TrackLookup}}, []string{item.ExternalID}); err != nil {
			return err
		}
	}
	return nil
}
