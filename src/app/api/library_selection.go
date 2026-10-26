package api

import (
	"net/http"

	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/library"
)

func (s *Server) registerTrackSelection(service *library.Service) {
	s.Handle("POST /api/v1/source-tracks/selection", true, true, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		var request library.TrackSelection
		if err := DecodeJSON(w, r, &request); err != nil {
			return err
		}
		result, err := service.SelectTracks(r.Context(), session.User.ID, request)
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, result)
	})
}
