package api

import (
	"net/http"

	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/metadata/onlinescrape"
)

func (s *Server) registerAlbumAutoSettings(catalog *onlinescrape.Service) {
	const path = "/api/v1/scrapers/album-auto-settings"
	s.Handle("GET "+path, true, false, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		page, err := catalog.AlbumAutoSettings(r.Context(), session.User.ID)
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, page)
	})
	s.Handle("PUT "+path, true, true, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		var input onlinescrape.AlbumAutoSettingsInput
		if err := DecodeJSON(w, r, &input); err != nil {
			return err
		}
		page, err := catalog.SaveAlbumAutoSettings(r.Context(), session.User.ID, input, r.Header.Get("Idempotency-Key"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, page)
	})
}
