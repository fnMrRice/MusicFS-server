package api

import (
	"net/http"

	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/music"
	"github.com/fnMrRice/MusicFS-server/src/library/works"
	"github.com/fnMrRice/MusicFS-server/src/metadata/onlinescrape"
)

// 对比预览只读；此接口仅发布用户明确勾选的字段。
func (s *Server) registerAlbumMetadataMerge(catalog *onlinescrape.Service, repo music.Repository) {
	s.Handle("PUT /api/v1/library/albums/{id}/metadata-merge", true, true, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		var input struct {
			ExpectedRevision *int64 `json:"expectedRevision"`
			PluginID         string `json:"pluginId"`
			Candidates       struct {
				Details string `json:"albumDetails"`
				Tracks  string `json:"albumTracks"`
			} `json:"candidates"`
			Fields []string `json:"fields"`
		}
		if err := DecodeJSON(w, r, &input); err != nil {
			return err
		}
		if input.ExpectedRevision == nil || *input.ExpectedRevision < 1 || input.Candidates.Details == "" || input.Candidates.Tracks == "" {
			return domain.Fail("INVALID_ARGUMENT", "Album revision and complete comparison candidates are required")
		}
		candidates := []works.ScrapedField{}
		for _, item := range []struct{ field, token string }{{"albumDetails", input.Candidates.Details}, {"albumTracks", input.Candidates.Tracks}} {
			candidate, err := catalog.Candidate(session.User.ID, input.PluginID, item.field, item.token)
			if err != nil {
				candidate = works.ScrapedField{Field: item.field, PluginID: input.PluginID}
			}
			candidate.CandidateToken = item.token
			candidates = append(candidates, candidate)
		}
		revision, err := repo.MergeMusicAlbumMetadata(r.Context(), session.User.ID, r.PathValue("id"), *input.ExpectedRevision, candidates, input.Fields, r.Header.Get("Idempotency-Key"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, map[string]int64{"revision": revision})
	})
}
