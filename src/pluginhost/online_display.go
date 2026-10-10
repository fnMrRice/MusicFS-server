package pluginhost

import (
	"context"
	"github.com/fnMrRice/MusicFS-server/src/online"
	"github.com/fnMrRice/MusicFS-server/src/pluginproto"
)

func (w *OnlineWorker) MetadataBatch(ctx context.Context, ids []string) (online.TrackBatch, error) {
	scope, err := w.newScope(ctx)
	if err != nil {
		return online.TrackBatch{}, err
	}
	result, err := onlineCall[online.TrackBatch](ctx, w, "provider.online.metadataBatch", scope, pluginproto.OnlineTrackBatch{Context: scope, ExternalIDs: ids})
	if err == nil {
		err = online.ValidateTrackBatch(w.config.Platform, result, ids)
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
		err = online.ValidatePlaylistTracks(w.config.Platform, result, r)
	}
	if err != nil {
		return online.PlaylistTracksPage{}, err
	}
	return result, nil
}
