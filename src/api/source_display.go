package api

import (
	"github.com/fnMrRice/MusicFS-server/src/auth"
	"github.com/fnMrRice/MusicFS-server/src/playlists"
	"github.com/fnMrRice/MusicFS-server/src/sourcedisplay"
	"net/http"
)

func (s *Server) RegisterSourceDisplay(service *sourcedisplay.Service) {
	s.Handle("POST /api/v1/source-refs/lookup", true, true, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		if _, err := onlineQuery(r); err != nil {
			return err
		}
		var input struct {
			Sources []playlists.SourceRef `json:"sources"`
		}
		if err := DecodeJSON(w, r, &input); err != nil {
			return err
		}
		result, err := service.Lookup(r.Context(), session.User.ID, input.Sources)
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, result)
	})
}
