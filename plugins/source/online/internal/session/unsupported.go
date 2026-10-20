package session

import (
	"context"

	"github.com/fnMrRice/MusicFS-server/sdk/fault"
	"github.com/fnMrRice/MusicFS-server/sdk/online"
)

// Unsupported lets drivers implement only their verified operations.
type Unsupported struct{}

func unsupported() error {
	return fault.Fail("UNSUPPORTED", "This capability has not been verified for this platform")
}

func (Unsupported) Begin(context.Context, online.AccountRequest) error { return unsupported() }
func (Unsupported) Poll(context.Context, online.AccountRequest) error  { return unsupported() }
func (Unsupported) Check(context.Context, *Secret) error               { return unsupported() }
func (Unsupported) AvatarURL(string) string                            { return "" }
func (Unsupported) Request(context.Context, *Secret, string, map[string]any, any) error {
	return unsupported()
}
func (Unsupported) Search(context.Context, online.SearchRequest, int) ([]online.Track, bool, error) {
	return nil, false, unsupported()
}
func (Unsupported) SearchEntities(context.Context, online.SearchRequest) (online.SearchPage, error) {
	return online.SearchPage{}, unsupported()
}
func (Unsupported) Metadata(context.Context, string) (online.Track, error) {
	return online.Track{}, unsupported()
}
func (Unsupported) ObservePlaylist(context.Context, string) (online.PlaylistObservation, error) {
	return online.PlaylistObservation{}, unsupported()
}
func (Unsupported) Album(context.Context, string) (online.Album, error) {
	return online.Album{}, unsupported()
}
func (Unsupported) Artist(context.Context, string) (online.Artist, error) {
	return online.Artist{}, unsupported()
}
func (Unsupported) AlbumTracks(context.Context, online.EntityPageRequest) (online.SearchPage, error) {
	return online.SearchPage{}, unsupported()
}
func (Unsupported) ArtistAlbums(context.Context, online.EntityPageRequest) (online.SearchPage, error) {
	return online.SearchPage{}, unsupported()
}
func (Unsupported) ArtistTracks(context.Context, online.EntityPageRequest) (online.SearchPage, error) {
	return online.SearchPage{}, unsupported()
}
func (Unsupported) MetadataBatch(context.Context, []string) (online.TrackBatch, error) {
	return online.TrackBatch{}, unsupported()
}
func (Unsupported) PlaylistTracks(context.Context, online.PlaylistTracksRequest) (online.PlaylistTracksPage, error) {
	return online.PlaylistTracksPage{}, unsupported()
}
func (Unsupported) Playlists(context.Context, online.PlaylistsRequest) (online.PlaylistsPage, error) {
	return online.PlaylistsPage{}, unsupported()
}
func (Unsupported) PublicRecommendations(context.Context, int) (online.RecommendationPage, error) {
	return online.RecommendationPage{}, unsupported()
}
