package pluginhost

import (
	"context"
	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
)

func (w *OnlineWorker) MetadataBatch(ctx context.Context, ids []string) (online.TrackBatch, error) {
	scope, err := w.newScope(ctx)
	if err != nil {
		return online.TrackBatch{}, err
	}
	result, err := onlineCall[online.TrackBatch](ctx, w, "provider.online.metadataBatch", scope, pluginproto.OnlineTrackBatch{Context: scope, ExternalIDs: ids})
	if err == nil {
		err = online.ValidateTrackBatch(w.descriptor.Resources, result, ids)
	}
	if err != nil {
		return online.TrackBatch{}, err
	}
	return result, nil
}
func (w *OnlineWorker) PlaylistTracks(ctx context.Context, r online.PlaylistTracksRequest) (online.PlaylistTracksPage, error) {
	scope, err := w.newScope(ctx)
	if err != nil {
		return online.PlaylistTracksPage{}, err
	}
	result, err := onlineCall[online.PlaylistTracksPage](ctx, w, "provider.online.playlistTracks", scope, pluginproto.OnlinePlaylistTracks{Context: scope, PlaylistTracksRequest: r})
	if err == nil {
		err = online.ValidatePlaylistTracks(w.descriptor.Resources, result, r)
	}
	if err != nil {
		return online.PlaylistTracksPage{}, err
	}
	return result, nil
}

func (w *OnlineWorker) Playlists(ctx context.Context, r online.PlaylistsRequest) (online.PlaylistsPage, error) {
	scope, err := w.newScope(ctx)
	if err != nil {
		return online.PlaylistsPage{}, err
	}
	result, err := onlineCall[online.PlaylistsPage](ctx, w, "provider.online.playlists", scope, pluginproto.OnlinePlaylists{Context: scope, PlaylistsRequest: r})
	if err == nil {
		err = online.ValidatePlaylists(w.descriptor.Resources, result, r.Limit)
	}
	if err != nil {
		return online.PlaylistsPage{}, err
	}
	return result, nil
}
