package adapter

import sdkonline "github.com/fnMrRice/MusicFS-server/sdk/online"

const (
	Netease            = sdkonline.Netease
	QQMusic            = sdkonline.QQMusic
	AppleMusic         = sdkonline.AppleMusic
	Unversioned        = sdkonline.Unversioned
	MaxPageItems       = sdkonline.MaxPageItems
	MaxPlaylistEntries = sdkonline.MaxPlaylistEntries
)

type AccountView = sdkonline.AccountView
type AccountRequest = sdkonline.AccountRequest
type AccountResult = sdkonline.AccountResult
type Album = sdkonline.Album
type Artist = sdkonline.Artist
type EntityPageRequest = sdkonline.EntityPageRequest
type TrackLookup = sdkonline.TrackLookup
type TrackBatch = sdkonline.TrackBatch
type PlaylistTracksRequest = sdkonline.PlaylistTracksRequest
type PlaylistTrack = sdkonline.PlaylistTrack
type PlaylistTracksPage = sdkonline.PlaylistTracksPage
type PlaylistsRequest = sdkonline.PlaylistsRequest
type PlaylistsPage = sdkonline.PlaylistsPage
type Scope = sdkonline.Scope
type Capability = sdkonline.Capability
type Track = sdkonline.Track
type SearchRequest = sdkonline.SearchRequest
type SearchPage = sdkonline.SearchPage
type Playlist = sdkonline.Playlist
type Recommendation = sdkonline.Recommendation
type RecommendationPage = sdkonline.RecommendationPage
type PlaylistEntry = sdkonline.PlaylistEntry
type PlaylistObservation = sdkonline.PlaylistObservation

func Capabilities(platform string) []Capability { return sdkonline.Capabilities(platform) }
func SearchKind(kind string) bool               { return sdkonline.SearchKind(kind) }
func ValidateTrack(platform string, track Track) error {
	return sdkonline.ValidateTrack(platform, track)
}
func ValidateSearch(platform string, page SearchPage, limit int) error {
	return sdkonline.ValidateSearch(platform, page, limit)
}
func ValidateRecommendations(platform string, page RecommendationPage, limit int) error {
	return sdkonline.ValidateRecommendations(platform, page, limit)
}
func ValidateObservation(platform string, page PlaylistObservation, id string) error {
	return sdkonline.ValidateObservation(platform, page, id)
}
func ValidateAlbum(platform string, album Album) error {
	return sdkonline.ValidateAlbum(platform, album)
}
func ValidateArtist(platform string, artist Artist) error {
	return sdkonline.ValidateArtist(platform, artist)
}
func ValidateTrackBatch(platform string, batch TrackBatch, ids []string) error {
	return sdkonline.ValidateTrackBatch(platform, batch, ids)
}
func ValidatePlaylistTracks(platform string, page PlaylistTracksPage, request PlaylistTracksRequest) error {
	return sdkonline.ValidatePlaylistTracks(platform, page, request)
}
func ValidatePlaylists(platform string, page PlaylistsPage, limit int) error {
	return sdkonline.ValidatePlaylists(platform, page, limit)
}
func textValue(value string, limit int) (string, error) { return sdkonline.TextValue(value, limit) }
func digest(value any) string                           { return sdkonline.Digest(value) }
func upstreamInvalid() error                            { return sdkonline.UpstreamInvalid() }
func imageURL(value string) (*string, error)            { return sdkonline.ImageURL(value) }
func validCount(value *int64) bool                      { return sdkonline.ValidCount(value) }
func playlistDigest(playlist Playlist, entries []PlaylistEntry) string {
	return sdkonline.PlaylistDigest(playlist, entries)
}
