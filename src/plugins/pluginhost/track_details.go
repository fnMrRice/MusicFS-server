package pluginhost

import (
	"context"
	"slices"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

func (w *Worker) TrackDetails(ctx context.Context, request pluginproto.ResolveRequest) (pluginproto.TrackDetails, error) {
	if err := w.state.ready(); err != nil {
		return pluginproto.TrackDetails{}, err
	}
	if !slices.Contains(w.Manifest.Capabilities, "track.details") {
		return pluginproto.TrackDetails{}, domain.Fail("UNSUPPORTED", "Provider does not support track details")
	}
	var result pluginproto.TrackDetails
	if err := w.peer.Call(ctx, "provider.trackDetails", request, &result); err != nil {
		return pluginproto.TrackDetails{}, err
	}
	if err := result.Validate(request.ExternalID); err != nil {
		return pluginproto.TrackDetails{}, err
	}
	return result, nil
}

func (w *OnlineWorker) TrackDetails(ctx context.Context, id string) (pluginproto.TrackDetails, error) {
	scope, err := w.newScope(ctx)
	if err != nil {
		return pluginproto.TrackDetails{}, err
	}
	result, err := onlineCall[pluginproto.TrackDetails](ctx, w, "provider.online.trackDetails", scope, pluginproto.OnlineReference{Context: scope, ExternalID: id})
	if err == nil && result.Validate(id) != nil {
		err = domain.Fail("PROTOCOL_MISMATCH", "Invalid provider track details")
	}
	if err != nil {
		return pluginproto.TrackDetails{}, err
	}
	return result, nil
}
