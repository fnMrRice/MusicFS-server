package api

import (
	"net/http"

	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/library"
)

func (s *Server) registerRootStatistics(service *library.Service) {
	s.Handle("GET /api/v1/provider-instances/{id}/root-statistics", true, false, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		statistics, err := service.RootStatistics(r.Context(), session.User.ID, r.PathValue("id"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, statistics)
	})
}
