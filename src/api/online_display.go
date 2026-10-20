package api

import (
	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/src/auth"
	"github.com/fnMrRice/MusicFS-server/src/onlinehost"
	"net/http"
)

func (s *Server) registerOnlineDisplay(service *onlinehost.Service) {
	s.Handle("POST /api/v1/online-instances/{id}/tracks/lookup", true, true, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		if _, err := onlineQuery(r); err != nil {
			return err
		}
		var input struct {
			ExternalIDs []string `json:"externalIds"`
		}
		if err := DecodeJSON(w, r, &input); err != nil {
			return err
		}
		result, err := service.MetadataBatch(r.Context(), session.User.ID, r.PathValue("id"), input.ExternalIDs)
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, result)
	})
	s.Handle("GET /api/v1/online-instances/{id}/playlists/{externalId}/tracks", true, false, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		query, err := onlineQuery(r, "limit", "cursor")
		if err != nil {
			return err
		}
		limit, err := onlineLimit(query)
		if err != nil {
			return err
		}
		result, err := service.PlaylistTracks(r.Context(), session.User.ID, r.PathValue("id"), online.PlaylistTracksRequest{ExternalID: r.PathValue("externalId"), Limit: limit, Cursor: query.Get("cursor")})
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, result)
	})
}
