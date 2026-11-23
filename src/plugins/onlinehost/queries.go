package onlinehost

import (
	"context"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

var externalID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9:_.-]{0,127}$`)

func (s *Service) Search(ctx context.Context, owner, id string, request online.SearchRequest) (online.SearchPage, error) {
	if request.Kind == "" {
		request.Kind = "tracks"
	}
	request.Query = strings.TrimSpace(request.Query)
	if !online.SearchKind(request.Kind) || request.Query == "" || len(request.Query) > 256 || !utf8.ValidString(request.Query) || strings.ContainsFunc(request.Query, unicode.IsControl) || request.Limit < 1 || request.Limit > online.MaxPageItems || len(request.Cursor) > 1024 {
		return online.SearchPage{}, domain.Fail("INVALID_ARGUMENT", "Invalid online search request")
	}
	return call(ctx, s, owner, id, func(ctx context.Context, r Runtime) (online.SearchPage, error) {
		return r.Provider.Search(ctx, request)
	})
}

func (s *Service) Metadata(ctx context.Context, owner, id, external string) (online.Track, error) {
	if !externalID.MatchString(external) {
		return online.Track{}, domain.Fail("INVALID_ARGUMENT", "Invalid external track identity")
	}
	return call(ctx, s, owner, id, func(ctx context.Context, r Runtime) (online.Track, error) {
		return r.Provider.Metadata(ctx, external)
	})
}

func (s *Service) ObservePlaylist(ctx context.Context, owner, id, external string) (online.PlaylistObservation, error) {
	if !externalID.MatchString(external) {
		return online.PlaylistObservation{}, domain.Fail("INVALID_ARGUMENT", "Invalid external playlist identity")
	}
	return call(ctx, s, owner, id, func(ctx context.Context, r Runtime) (online.PlaylistObservation, error) {
		return r.Provider.ObservePlaylist(ctx, external)
	})
}

func (s *Service) PublicRecommendations(ctx context.Context, owner, id string, limit int) (online.RecommendationPage, error) {
	if limit < 1 || limit > online.MaxPageItems {
		return online.RecommendationPage{}, domain.Fail("INVALID_ARGUMENT", "Invalid recommendation limit")
	}
	return call(ctx, s, owner, id, func(ctx context.Context, r Runtime) (online.RecommendationPage, error) {
		return r.Provider.PublicRecommendations(ctx, limit)
	})
}

func (s *Service) DailySongs(ctx context.Context, owner, id string, limit int) (online.DailySongsPage, error) {
	if limit < 1 || limit > online.MaxPageItems {
		return online.DailySongsPage{}, domain.Fail("INVALID_ARGUMENT", "Invalid daily song limit")
	}
	return call(ctx, s, owner, id, func(ctx context.Context, r Runtime) (online.DailySongsPage, error) {
		provider, ok := r.Provider.(interface {
			DailySongs(context.Context, int) (online.DailySongsPage, error)
		})
		if !ok {
			return online.DailySongsPage{}, domain.Fail("UNSUPPORTED", "Provider does not support daily song recommendations")
		}
		return provider.DailySongs(ctx, limit)
	})
}
