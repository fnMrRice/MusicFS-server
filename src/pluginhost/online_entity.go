package pluginhost

import (
	"context"
	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/domain"
)

func (w *OnlineWorker) Album(ctx context.Context, id string) (online.Album, error) {
	scope, err := w.newScope(ctx)
	if err != nil {
		return online.Album{}, err
	}
	value, err := onlineCall[online.Album](ctx, w, "provider.online.album", scope, pluginproto.OnlineReference{Context: scope, ExternalID: id})
	if err == nil {
		err = online.ValidateAlbum(w.config.Platform, value)
	}
	if err == nil && value.ExternalID != id {
		err = domain.Fail("PROTOCOL_MISMATCH", "Album identity mismatch")
	}
	if err != nil {
		return online.Album{}, err
	}
	return value, nil
}
func (w *OnlineWorker) Artist(ctx context.Context, id string) (online.Artist, error) {
	scope, err := w.newScope(ctx)
	if err != nil {
		return online.Artist{}, err
	}
	value, err := onlineCall[online.Artist](ctx, w, "provider.online.artist", scope, pluginproto.OnlineReference{Context: scope, ExternalID: id})
	if err == nil {
		err = online.ValidateArtist(w.config.Platform, value)
	}
	if err == nil && value.ExternalID != id {
		err = domain.Fail("PROTOCOL_MISMATCH", "Artist identity mismatch")
	}
	if err != nil {
		return online.Artist{}, err
	}
	return value, nil
}
func (w *OnlineWorker) entityPage(ctx context.Context, method, kind string, r online.EntityPageRequest) (online.SearchPage, error) {
	scope, err := w.newScope(ctx)
	if err != nil {
		return online.SearchPage{}, err
	}
	value, err := onlineCall[online.SearchPage](ctx, w, method, scope, pluginproto.OnlineEntityPage{Context: scope, EntityPageRequest: r})
	if err == nil {
		err = online.ValidateSearch(w.config.Platform, value, r.Limit)
	}
	if err == nil && value.Kind != kind {
		err = domain.Fail("PROTOCOL_MISMATCH", "Entity relationship kind mismatch")
	}
	if err != nil {
		return online.SearchPage{}, err
	}
	return value, nil
}
func (w *OnlineWorker) AlbumTracks(ctx context.Context, r online.EntityPageRequest) (online.SearchPage, error) {
	return w.entityPage(ctx, "provider.online.albumTracks", "tracks", r)
}
func (w *OnlineWorker) ArtistAlbums(ctx context.Context, r online.EntityPageRequest) (online.SearchPage, error) {
	return w.entityPage(ctx, "provider.online.artistAlbums", "albums", r)
}
func (w *OnlineWorker) ArtistTracks(ctx context.Context, r online.EntityPageRequest) (online.SearchPage, error) {
	return w.entityPage(ctx, "provider.online.artistTracks", "tracks", r)
}
