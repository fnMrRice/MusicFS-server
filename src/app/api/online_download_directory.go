package api

import (
	"net/http"

	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/playback"
)

func (s *Server) registerDownloadDirectory(service *playback.Service) {
	const path = "/api/v1/admin/online-download-directory"
	s.Handle("GET "+path, true, false, requireAdmin(func(w http.ResponseWriter, r *http.Request, _ auth.Session) error {
		directory, err := service.DownloadDirectory()
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, directory)
	}))
	s.Handle("PUT "+path, true, true, requireAdmin(func(w http.ResponseWriter, r *http.Request, _ auth.Session) error {
		var body struct {
			Path *string `json:"path"`
		}
		if err := DecodeJSON(w, r, &body); err != nil {
			return err
		}
		if body.Path == nil {
			return domain.Fail("INVALID_ARGUMENT", "A download directory path is required")
		}
		directory, err := service.SetDownloadDirectory(*body.Path)
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, directory)
	}))
}
