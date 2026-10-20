package api

import (
	"net/http"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/src/auth"
	"github.com/fnMrRice/MusicFS-server/src/onlinehost"
)

func (s *Server) registerOnlinePlaylists(service *onlinehost.Service) {
	s.Handle("GET /api/v1/online-instances/{id}/playlists", true, false, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		query, err := onlineQuery(r, "limit", "cursor")
		if err != nil {
			return err
		}
		limit, err := onlineLimit(query)
		if err != nil {
			return err
		}
		page, err := service.Playlists(r.Context(), session.User.ID, r.PathValue("id"), online.PlaylistsRequest{Limit: limit, Cursor: query.Get("cursor")})
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, page)
	})
}
