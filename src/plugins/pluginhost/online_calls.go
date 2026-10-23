package pluginhost

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

func (w *OnlineWorker) newScope(ctx context.Context) (pluginproto.Context, error) {
	if err := ctx.Err(); err != nil {
		return pluginproto.Context{}, err
	}
	select {
	case <-w.done:
		return pluginproto.Context{}, domain.Fail("SOURCE_OFFLINE", "Online worker is offline")
	default:
	}
	timeout := w.config.Timeout
	if deadline, ok := ctx.Deadline(); ok {
		timeout = min(timeout, time.Until(deadline))
	}
	if timeout < time.Millisecond {
		return pluginproto.Context{}, context.DeadlineExceeded
	}
	task, err := onlineRandom()
	return pluginproto.Context{InstanceID: w.config.InstanceID, UserID: w.config.UserID, TaskID: task, TimeoutMs: timeout.Milliseconds()}, err
}

func onlineCall[T any](parent context.Context, w *OnlineWorker, method string, scope pluginproto.Context, request any) (T, error) {
	var empty T
	ctx, cancel := context.WithTimeout(parent, time.Duration(scope.TimeoutMs)*time.Millisecond)
	defer cancel()
	if err := w.requireOnlineCapability(method, request); err != nil {
		if w.descriptor.Authentication == "anonymous" {
			return empty, err
		}
		view, authErr := w.PlatformSession(ctx)
		if authErr != nil {
			return empty, authErr
		}
		if view.State != "signedIn" || view.AccountID == "" || view.Revision == "" {
			return empty, domain.Fail("PLATFORM_AUTH_REQUIRED", "Sign in to the music platform")
		}
		return empty, err
	}
	var raw json.RawMessage
	if err := w.peer.Call(ctx, method, request, &raw); err != nil {
		failure := onlineError(ctx, err)
		w.accountDisplay.invalidateOnError(failure)
		return empty, failure
	}
	var response pluginproto.OnlineResult[T]
	if pluginproto.DecodeOnline(raw, &response) != nil || response.Context != scope {
		w.cancel()
		return empty, domain.Fail("PROTOCOL_MISMATCH", "Online result scope mismatch")
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	return response.Value, nil
}

func (w *OnlineWorker) Describe(ctx context.Context) (pluginproto.OnlineDescriptor, error) {
	scope, err := w.newScope(ctx)
	if err != nil {
		return pluginproto.OnlineDescriptor{}, err
	}
	d, err := onlineCall[pluginproto.OnlineDescriptor](ctx, w, "provider.online.describe", scope, pluginproto.OnlineScope{Context: scope})
	if err == nil {
		if validation := pluginproto.ValidateOnlineDescriptor(w.manifest, d, w.config.Platform); validation != nil {
			return pluginproto.OnlineDescriptor{}, domain.Fail("PROTOCOL_MISMATCH", "Online plugin descriptor is invalid")
		}
	}
	return d, err
}

func (w *OnlineWorker) Search(ctx context.Context, r online.SearchRequest) (online.SearchPage, error) {
	scope, err := w.newScope(ctx)
	if err != nil {
		return online.SearchPage{}, err
	}
	page, err := onlineCall[online.SearchPage](ctx, w, "provider.online.search", scope, pluginproto.OnlineSearch{Context: scope, Kind: r.Kind, Query: r.Query, Limit: r.Limit, Cursor: r.Cursor})
	if err == nil {
		err = online.ValidateSearch(w.descriptor.Resources, page, r.Limit)
		want := r.Kind
		if want == "" {
			want = "tracks"
		}
		if err == nil && page.Kind != want {
			err = domain.Fail("PROTOCOL_MISMATCH", "Online result kind mismatch")
		}
	}
	if err != nil {
		return online.SearchPage{}, err
	}
	return page, nil
}

func (w *OnlineWorker) Metadata(ctx context.Context, id string) (online.Track, error) {
	scope, err := w.newScope(ctx)
	if err != nil {
		return online.Track{}, err
	}
	track, err := onlineCall[online.Track](ctx, w, "provider.online.metadata", scope, pluginproto.OnlineReference{Context: scope, ExternalID: id})
	if err == nil {
		err = online.ValidateTrack(w.descriptor.Resources, track)
		if err == nil && track.ExternalID != id {
			err = domain.Fail("PROTOCOL_MISMATCH", "Online track identity mismatch")
		}
	}
	if err != nil {
		return online.Track{}, err
	}
	return track, nil
}

func (w *OnlineWorker) ObservePlaylist(ctx context.Context, id string) (online.PlaylistObservation, error) {
	scope, err := w.newScope(ctx)
	if err != nil {
		return online.PlaylistObservation{}, err
	}
	page, err := onlineCall[online.PlaylistObservation](ctx, w, "provider.online.playlist", scope, pluginproto.OnlineReference{Context: scope, ExternalID: id})
	if err == nil {
		err = online.ValidateObservation(w.descriptor.Resources, page, id)
	}
	if err != nil {
		return online.PlaylistObservation{}, err
	}
	return page, nil
}

func (w *OnlineWorker) PublicRecommendations(ctx context.Context, limit int) (online.RecommendationPage, error) {
	scope, err := w.newScope(ctx)
	if err != nil {
		return online.RecommendationPage{}, err
	}
	page, err := onlineCall[online.RecommendationPage](ctx, w, "provider.online.recommendations", scope, pluginproto.OnlineRecommendations{Context: scope, Limit: limit})
	if err == nil {
		err = online.ValidateRecommendations(w.descriptor.Resources, page, limit)
	}
	if err != nil {
		return online.RecommendationPage{}, err
	}
	return page, nil
}

func (w *OnlineWorker) Resolve(ctx context.Context, id string) error {
	_, err := w.DescribeAudio(ctx, id)
	return err
}

func onlineError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var rpc *pluginproto.RPCError
	if errors.As(err, &rpc) {
		code := rpc.Data.Code
		switch code {
		case "INVALID_ARGUMENT", "CURSOR_EXPIRED", "ACCESS_DENIED", "AUTH_REQUIRED", "PLATFORM_AUTH_REQUIRED", "NOT_FOUND", "RATE_LIMITED", "RISK_CONTROL", "RESOURCE_CHANGED", "TEMPORARY_FAILURE", "UNSUPPORTED", "PROTOCOL_MISMATCH", "CANCELED":
		default:
			code = "TEMPORARY_FAILURE"
		}
		failure := &domain.Error{Code: code, Message: "Online provider request failed"}
		if ms := rpc.Data.RetryAfterMs; ms != nil && *ms > 0 && *ms <= 3600000 {
			seconds := int((*ms + 999) / 1000)
			failure.RetryAfter = &seconds
		}
		return failure
	}
	return domain.Fail("SOURCE_OFFLINE", "Online provider control channel is unavailable")
}
