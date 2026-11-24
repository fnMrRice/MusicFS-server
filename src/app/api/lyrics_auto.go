package api

import (
	"net/http"

	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/metadata/onlinescrape"
)

func (s *Server) registerLyricsAuto(catalog *onlinescrape.Service) {
	const path = "/api/v1/scrapers/lyrics-auto-settings"
	s.Handle("GET "+path, true, false, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		settings, err := catalog.LyricsAutoSettings(r.Context(), session.User.ID)
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, settings)
	})
	s.Handle("PUT "+path, true, true, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		var input onlinescrape.LyricsAutoSettingsInput
		if err := DecodeJSON(w, r, &input); err != nil {
			return err
		}
		settings, err := catalog.SaveLyricsAutoSettings(r.Context(), session.User.ID, input, r.Header.Get("Idempotency-Key"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, settings)
	})
	s.Handle("POST /api/v1/scrapers/lyrics-auto-search", true, true, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		var input struct {
			Query         *scraper.Query `json:"query"`
			SourceTrackID string         `json:"sourceTrackId,omitempty"`
			LocalOnly     bool           `json:"localOnly,omitempty"`
		}
		if err := DecodeJSON(w, r, &input); err != nil {
			return err
		}
		if input.Query == nil {
			return domain.Fail("INVALID_ARGUMENT", "Lyrics search query is required")
		}
		result, err := catalog.SearchLyrics(r.Context(), session.User.ID, onlinescrape.LyricsAutoSearchInput{Query: *input.Query, SourceTrackID: input.SourceTrackID, LocalOnly: input.LocalOnly}, nil)
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, result)
	})
}
