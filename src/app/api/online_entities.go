package api

import (
	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/plugins/onlinehost"
	"net/http"
)

func (s *Server) registerOnlineEntities(service *onlinehost.Service) {
	s.Handle("GET /api/v1/online-instances/{id}/albums/{externalId}", true, false, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		if _, err := onlineQuery(r); err != nil {
			return err
		}
		value, err := service.Album(r.Context(), session.User.ID, r.PathValue("id"), r.PathValue("externalId"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, value)
	})
	s.Handle("GET /api/v1/online-instances/{id}/artists/{externalId}", true, false, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		if _, err := onlineQuery(r); err != nil {
			return err
		}
		value, err := service.Artist(r.Context(), session.User.ID, r.PathValue("id"), r.PathValue("externalId"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, value)
	})
	for _, route := range []struct{ path, kind string }{{"albums/{externalId}/tracks", "albumTracks"}, {"artists/{externalId}/albums", "artistAlbums"}, {"artists/{externalId}/tracks", "artistTracks"}} {
		s.Handle("GET /api/v1/online-instances/{id}/"+route.path, true, false, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
			q, err := onlineQuery(r, "limit", "cursor")
			if err != nil {
				return err
			}
			limit, err := onlineLimit(q)
			if err != nil {
				return err
			}
			page, err := service.EntityPage(r.Context(), session.User.ID, r.PathValue("id"), route.kind, online.EntityPageRequest{ExternalID: r.PathValue("externalId"), Limit: limit, Cursor: q.Get("cursor")})
			if err != nil {
				return err
			}
			return WriteJSON(w, http.StatusOK, page)
		})
	}
}
