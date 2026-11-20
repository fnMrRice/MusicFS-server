package api

import (
	"net/http"

	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/works"
	"github.com/fnMrRice/MusicFS-server/src/metadata/onlinescrape"
)

type scrapedFieldInput struct {
	PluginID         string `json:"pluginId"`
	CandidateToken   string `json:"candidateToken"`
	ExpectedRevision *int64 `json:"expectedRevision"`
}

func (s *Server) RegisterOnlineScrapers(catalog *onlinescrape.Service, repo works.Repository, media onlineTrackMedia, client *http.Client) {
	s.Handle("GET /api/v1/scrapers", true, false, func(w http.ResponseWriter, r *http.Request, _ auth.Session) error {
		return WriteJSON(w, http.StatusOK, map[string]any{"items": catalog.List()})
	})
	s.Handle("POST /api/v1/scrapers/{id}/search", true, true, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		var input struct {
			Field   string        `json:"field"`
			Query   scraper.Query `json:"query"`
			Refresh bool          `json:"refresh"`
		}
		if err := DecodeJSON(w, r, &input); err != nil {
			return err
		}
		items, err := catalog.SearchWithRefresh(r.Context(), session.User.ID, r.PathValue("id"), input.Field, input.Query, input.Refresh)
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, map[string]any{"items": items})
	})
	s.Handle("GET /api/v1/works/{id}/scraped-fields", true, false, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		items, err := repo.WorkScrapedFields(r.Context(), session.User.ID, r.PathValue("id"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, map[string]any{"items": items})
	})
	s.Handle("PUT /api/v1/works/{id}/scraped-fields/{field}", true, true, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		var input scrapedFieldInput
		if err := DecodeJSON(w, r, &input); err != nil {
			return err
		}
		if input.ExpectedRevision == nil || *input.ExpectedRevision < 1 {
			return domain.Fail("INVALID_ARGUMENT", "Expected work revision is required")
		}
		selected, err := catalog.Candidate(session.User.ID, input.PluginID, r.PathValue("field"), input.CandidateToken)
		if err != nil {
			// 未命中短期预览仍可重放已成功的同一操作。存储层会拒绝新的选用。
			selected = works.ScrapedField{Field: r.PathValue("field"), PluginID: input.PluginID}
		}
		selected.CandidateToken = input.CandidateToken
		revision, err := repo.SelectWorkScrapedField(r.Context(), session.User.ID, r.PathValue("id"), *input.ExpectedRevision, selected, r.Header.Get("Idempotency-Key"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, map[string]int64{"revision": revision})
	})
	s.registerScrapedTrack(catalog, media, client)
}
