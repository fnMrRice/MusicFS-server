package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/instanceid"
	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/music"
	"github.com/fnMrRice/MusicFS-server/src/library/works"
	"github.com/fnMrRice/MusicFS-server/src/metadata/onlinescrape"
)

// RegisterOnlineAlbumScrapers 为曲目列表中的专辑保存逐字段刮削资料。
func (s *Server) RegisterOnlineAlbumScrapers(catalog *onlinescrape.Service, repo music.Repository) {
	s.registerAlbumMetadataMerge(catalog, repo)
	s.register("POST /api/v1/library/albums/{id}/auto-scrape", true, true, 90*time.Second, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		var input autoAlbumScope
		if err := DecodeJSON(w, r, &input); err != nil {
			return err
		}
		provider, err := input.provider()
		if err != nil {
			return err
		}
		items, err := catalog.SearchVerifiedAlbum(r.Context(), repo, session.User.ID, r.PathValue("id"), provider)
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, map[string]any{"items": items})
	})
	s.Handle("GET /api/v1/library/albums/{id}/scraped-fields", true, false, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		items, err := repo.MusicAlbumScrapedFields(r.Context(), session.User.ID, r.PathValue("id"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, map[string]any{"items": items})
	})
	s.Handle("DELETE /api/v1/library/albums/{id}/scraped-fields", true, true, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		var input struct {
			ExpectedRevision int64 `json:"expectedRevision"`
		}
		if err := DecodeJSON(w, r, &input); err != nil {
			return err
		}
		if input.ExpectedRevision < 1 {
			return domain.Fail("INVALID_ARGUMENT", "Expected album revision is required")
		}
		revision, err := repo.ClearMusicAlbumScrapedFields(r.Context(), session.User.ID, r.PathValue("id"), input.ExpectedRevision, r.Header.Get("Idempotency-Key"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, map[string]int64{"revision": revision})
	})
	s.Handle("PUT /api/v1/library/albums/{id}/scraped-fields", true, true, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		var input albumBundleInput
		if err := DecodeJSON(w, r, &input); err != nil {
			return err
		}
		fields, err := input.fields(catalog, session.User.ID)
		if err != nil {
			return err
		}
		revision, err := repo.SelectMusicAlbumScrapedFields(r.Context(), session.User.ID, r.PathValue("id"), *input.ExpectedRevision, fields, r.Header.Get("Idempotency-Key"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, map[string]int64{"revision": revision})
	})
	s.Handle("PUT /api/v1/library/albums/{id}/auto-scrape", true, true, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		var input struct {
			albumBundleInput
			autoAlbumScope
		}
		if err := DecodeJSON(w, r, &input); err != nil {
			return err
		}
		provider, err := input.provider()
		if err != nil {
			return err
		}
		fields, err := input.fields(catalog, session.User.ID)
		if err != nil {
			return err
		}
		revision, err := repo.SelectAutoMusicAlbumScrapedFields(r.Context(), session.User.ID, r.PathValue("id"), provider, *input.ExpectedRevision, fields, r.Header.Get("Idempotency-Key"))
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

type autoAlbumScope struct {
	ProviderInstanceID json.RawMessage `json:"providerInstanceId"`
}

func (input autoAlbumScope) provider() (string, error) {
	var provider string
	if input.ProviderInstanceID != nil && (json.Unmarshal(input.ProviderInstanceID, &provider) != nil || !instanceid.Valid(provider)) {
		return "", domain.Fail("INVALID_ARGUMENT", "Invalid provider instance identifier")
	}
	return provider, nil
}

type albumBundleInput struct {
	ExpectedRevision *int64 `json:"expectedRevision"`
	PluginID         string `json:"pluginId"`
	Candidates       struct {
		Details string `json:"albumDetails"`
		Tracks  string `json:"albumTracks"`
		Artwork string `json:"artwork"`
	} `json:"candidates"`
}

func (input albumBundleInput) fields(catalog *onlinescrape.Service, owner string) ([]works.ScrapedField, error) {
	if input.ExpectedRevision == nil || *input.ExpectedRevision < 1 || input.Candidates.Details == "" || input.Candidates.Tracks == "" {
		return nil, domain.Fail("INVALID_ARGUMENT", "Album revision, details and complete track list are required")
	}
	var fields []works.ScrapedField
	for _, candidate := range []struct{ field, token string }{{"albumDetails", input.Candidates.Details}, {"albumTracks", input.Candidates.Tracks}, {"artwork", input.Candidates.Artwork}} {
		if candidate.token == "" {
			continue
		}
		field, err := catalog.Candidate(owner, input.PluginID, candidate.field, candidate.token)
		if err != nil {
			// 候选过期后，存储层仍可按原令牌重放已成功的请求。
			field = works.ScrapedField{Field: candidate.field, PluginID: input.PluginID}
		}
		field.CandidateToken = candidate.token
		fields = append(fields, field)
	}
	return fields, nil
}
