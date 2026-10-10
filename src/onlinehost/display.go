package onlinehost

import (
	"context"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/online"
)

type DisplayProvider interface {
	MetadataBatch(context.Context, []string) (online.TrackBatch, error)
	PlaylistTracks(context.Context, online.PlaylistTracksRequest) (online.PlaylistTracksPage, error)
}

func (s *Service) MetadataBatch(ctx context.Context, owner, id string, ids []string) (online.TrackBatch, error) {
	if len(ids) < 1 || len(ids) > online.MaxPageItems {
		return online.TrackBatch{}, domain.Fail("INVALID_ARGUMENT", "Invalid track batch size")
	}
	for _, id := range ids {
		if !externalID.MatchString(id) {
			return online.TrackBatch{}, domain.Fail("INVALID_ARGUMENT", "Invalid external track identity")
		}
	}
	return call(ctx, s, owner, id, func(ctx context.Context, r Runtime) (online.TrackBatch, error) {
		provider, ok := r.Provider.(DisplayProvider)
		if !ok {
			return online.TrackBatch{}, domain.Fail("UNSUPPORTED", "Provider does not support batched display metadata")
		}
		return provider.MetadataBatch(ctx, ids)
	})
}
func (s *Service) PlaylistTracks(ctx context.Context, owner, id string, request online.PlaylistTracksRequest) (online.PlaylistTracksPage, error) {
	if !externalID.MatchString(request.ExternalID) || request.Limit < 1 || request.Limit > online.MaxPageItems || len(request.Cursor) > 1024 {
		return online.PlaylistTracksPage{}, domain.Fail("INVALID_ARGUMENT", "Invalid playlist track page")
	}
	return call(ctx, s, owner, id, func(ctx context.Context, r Runtime) (online.PlaylistTracksPage, error) {
		provider, ok := r.Provider.(DisplayProvider)
		if !ok {
			return online.PlaylistTracksPage{}, domain.Fail("UNSUPPORTED", "Provider does not support playlist display metadata")
		}
		return provider.PlaylistTracks(ctx, request)
	})
}
