package api

import (
	"net/http"

	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/music"
	"github.com/fnMrRice/MusicFS-server/src/library/works"
	"github.com/fnMrRice/MusicFS-server/src/metadata/onlinescrape"
)

// RegisterOnlineAlbumScrapers 为曲目列表中的专辑保存逐字段刮削资料。
func (s *Server) RegisterOnlineAlbumScrapers(catalog *onlinescrape.Service, repo music.Repository) {
	s.Handle("GET /api/v1/library/albums/{id}/scraped-fields", true, false, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		items, err := repo.MusicAlbumScrapedFields(r.Context(), session.User.ID, r.PathValue("id"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, map[string]any{"items": items})
	})
	s.Handle("PUT /api/v1/library/albums/{id}/scraped-fields", true, true, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		var input struct {
			ExpectedRevision *int64 `json:"expectedRevision"`
			PluginID         string `json:"pluginId"`
			Candidates       struct {
				Details string `json:"albumDetails"`
				Tracks  string `json:"albumTracks"`
				Artwork string `json:"artwork"`
			} `json:"candidates"`
		}
		if err := DecodeJSON(w, r, &input); err != nil {
			return err
		}
		if input.ExpectedRevision == nil || *input.ExpectedRevision < 1 || input.Candidates.Details == "" || input.Candidates.Tracks == "" {
			return domain.Fail("INVALID_ARGUMENT", "Album revision, details and complete track list are required")
		}
		fields := []works.ScrapedField{}
		for _, candidate := range []struct{ field, token string }{{"albumDetails", input.Candidates.Details}, {"albumTracks", input.Candidates.Tracks}, {"artwork", input.Candidates.Artwork}} {
			if candidate.token == "" {
				continue
			}
			field, err := catalog.Candidate(session.User.ID, input.PluginID, candidate.field, candidate.token)
			if err != nil {
				field = works.ScrapedField{Field: candidate.field, PluginID: input.PluginID}
			}
			field.CandidateToken = candidate.token
			fields = append(fields, field)
		}
		revision, err := repo.SelectMusicAlbumScrapedFields(r.Context(), session.User.ID, r.PathValue("id"), *input.ExpectedRevision, fields, r.Header.Get("Idempotency-Key"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, map[string]int64{"revision": revision})
	})
	s.Handle("PUT /api/v1/library/albums/{id}/scraped-fields/{field}", true, true, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		var input scrapedFieldInput
		if err := DecodeJSON(w, r, &input); err != nil {
			return err
		}
		field := r.PathValue("field")
		if !music.AlbumScrapedField(field) || input.ExpectedRevision == nil || *input.ExpectedRevision < 1 {
			return domain.Fail("INVALID_ARGUMENT", "Album field and expected revision are required")
		}
		selected, err := catalog.Candidate(session.User.ID, input.PluginID, field, input.CandidateToken)
		if err != nil {
			// 失效候选只能重放已完成的同一请求；新选用由存储层拒绝。
			selected = works.ScrapedField{Field: field, PluginID: input.PluginID}
		}
		selected.CandidateToken = input.CandidateToken
		revision, err := repo.SelectMusicAlbumScrapedField(r.Context(), session.User.ID, r.PathValue("id"), *input.ExpectedRevision, selected, r.Header.Get("Idempotency-Key"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, map[string]int64{"revision": revision})
	})
}
