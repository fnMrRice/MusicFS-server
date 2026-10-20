package provider

import (
	"context"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
)

// UnimplementedClient can be embedded by providers that implement a subset of
// capabilities. Override Account to supply real account lifecycle handling.
// The descriptor must advertise only methods the provider actually implements.
type UnimplementedClient struct{}

func (UnimplementedClient) Close() {}

func (UnimplementedClient) Account(context.Context, online.AccountRequest) (online.AccountResult, error) {
	return online.AccountResult{}, unsupported()
}
func (UnimplementedClient) Search(context.Context, online.SearchRequest) (online.SearchPage, error) {
	return online.SearchPage{}, unsupported()
}
func (UnimplementedClient) Metadata(context.Context, string) (online.Track, error) {
	return online.Track{}, unsupported()
}
func (UnimplementedClient) ObservePlaylist(context.Context, string) (online.PlaylistObservation, error) {
	return online.PlaylistObservation{}, unsupported()
}
func (UnimplementedClient) Resolve(context.Context, string) error { return unsupported() }
func (UnimplementedClient) Album(context.Context, string) (online.Album, error) {
	return online.Album{}, unsupported()
}
func (UnimplementedClient) Artist(context.Context, string) (online.Artist, error) {
	return online.Artist{}, unsupported()
}
func (UnimplementedClient) AlbumTracks(context.Context, online.EntityPageRequest) (online.SearchPage, error) {
	return online.SearchPage{}, unsupported()
}
func (UnimplementedClient) ArtistAlbums(context.Context, online.EntityPageRequest) (online.SearchPage, error) {
	return online.SearchPage{}, unsupported()
}
func (UnimplementedClient) ArtistTracks(context.Context, online.EntityPageRequest) (online.SearchPage, error) {
	return online.SearchPage{}, unsupported()
}
func (UnimplementedClient) MetadataBatch(context.Context, []string) (online.TrackBatch, error) {
	return online.TrackBatch{}, unsupported()
}
func (UnimplementedClient) PlaylistTracks(context.Context, online.PlaylistTracksRequest) (online.PlaylistTracksPage, error) {
	return online.PlaylistTracksPage{}, unsupported()
}
func (UnimplementedClient) Playlists(context.Context, online.PlaylistsRequest) (online.PlaylistsPage, error) {
	return online.PlaylistsPage{}, unsupported()
}
func (UnimplementedClient) PublicRecommendations(context.Context, int) (online.RecommendationPage, error) {
	return online.RecommendationPage{}, unsupported()
}
func unsupported() error {
	return pluginproto.Failure("UNSUPPORTED", "This capability is not implemented by this plugin")
}
