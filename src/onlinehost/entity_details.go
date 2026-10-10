package onlinehost

import (
	"context"
	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/online"
)

type EntityProvider interface {
	Album(context.Context, string) (online.Album, error)
	Artist(context.Context, string) (online.Artist, error)
	AlbumTracks(context.Context, online.EntityPageRequest) (online.SearchPage, error)
	ArtistAlbums(context.Context, online.EntityPageRequest) (online.SearchPage, error)
	ArtistTracks(context.Context, online.EntityPageRequest) (online.SearchPage, error)
}

func (s *Service) Album(ctx context.Context, owner, id, external string) (online.Album, error) {
	if !externalID.MatchString(external) {
		return online.Album{}, domain.Fail("INVALID_ARGUMENT", "Invalid album identity")
	}
	return call(ctx, s, owner, id, func(ctx context.Context, r Runtime) (online.Album, error) {
		p, ok := r.Provider.(EntityProvider)
		if !ok {
			return online.Album{}, domain.Fail("UNSUPPORTED", "Album metadata is not supported")
		}
		return p.Album(ctx, external)
	})
}
func (s *Service) Artist(ctx context.Context, owner, id, external string) (online.Artist, error) {
	if !externalID.MatchString(external) {
		return online.Artist{}, domain.Fail("INVALID_ARGUMENT", "Invalid artist identity")
	}
	return call(ctx, s, owner, id, func(ctx context.Context, r Runtime) (online.Artist, error) {
		p, ok := r.Provider.(EntityProvider)
		if !ok {
			return online.Artist{}, domain.Fail("UNSUPPORTED", "Artist metadata is not supported")
		}
		return p.Artist(ctx, external)
	})
}
func (s *Service) EntityPage(ctx context.Context, owner, id, kind string, request online.EntityPageRequest) (online.SearchPage, error) {
	if !externalID.MatchString(request.ExternalID) || request.Limit < 1 || request.Limit > online.MaxPageItems || len(request.Cursor) > 1024 {
		return online.SearchPage{}, domain.Fail("INVALID_ARGUMENT", "Invalid entity page")
	}
	return call(ctx, s, owner, id, func(ctx context.Context, r Runtime) (online.SearchPage, error) {
		p, ok := r.Provider.(EntityProvider)
		if !ok {
			return online.SearchPage{}, domain.Fail("UNSUPPORTED", "Entity paging is not supported")
		}
		switch kind {
		case "albumTracks":
			return p.AlbumTracks(ctx, request)
		case "artistAlbums":
			return p.ArtistAlbums(ctx, request)
		case "artistTracks":
			return p.ArtistTracks(ctx, request)
		}
		return online.SearchPage{}, domain.Fail("INVALID_ARGUMENT", "Unknown entity relationship")
	})
}
