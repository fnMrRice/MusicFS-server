package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/metadata/matching"
)

type matchingAPI interface {
	MatchingRevision(context.Context, string) (int64, error)
	Refresh(context.Context, string, string, []string) (matching.CandidatePage, error)
	Candidates(context.Context, string, string, string, int) (matching.CandidatePage, error)
	Recording(context.Context, string, string) (matching.Recording, error)
	GetLink(context.Context, string, string) (matching.Link, error)
	ManualLink(context.Context, string, matching.LinkRequest, string) (matching.Mutation, error)
	Unlink(context.Context, string, matching.UnlinkRequest, string) (matching.Mutation, error)
	Split(context.Context, string, matching.SplitRequest, string) (matching.Mutation, error)
	SetExclusion(context.Context, string, matching.ExclusionRequest, string) (matching.Mutation, error)
	ChangesAfter(context.Context, string, int64, int) (matching.ChangePage, error)
}

func (s *Server) RegisterMatching(service matchingAPI) {
	s.Handle("GET /api/v1/matching/candidates", true, false, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		after, limit, err := matchingPage(r, 20, 100)
		if err != nil {
			return err
		}
		page, err := service.Candidates(r.Context(), session.User.ID, "", after, limit)
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, page)
	})
	s.Handle("GET /api/v1/matching/revision", true, false, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		revision, err := service.MatchingRevision(r.Context(), session.User.ID)
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, struct {
			Revision int64 `json:"revision"`
		}{revision})
	})
	s.Handle("POST /api/v1/source-tracks/{id}/match-candidates/refresh", true, true, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		var input struct {
			SourceTrackIDs json.RawMessage `json:"sourceTrackIds"`
		}
		if err := DecodeJSON(w, r, &input); err != nil {
			return err
		}
		var ids []string
		if len(input.SourceTrackIDs) > 0 {
			if string(input.SourceTrackIDs) == "null" || json.Unmarshal(input.SourceTrackIDs, &ids) != nil {
				return domain.Fail("INVALID_ARGUMENT", "sourceTrackIds must be an array")
			}
			if len(ids) > 0 {
				if err := matchingSourceIDs(ids); err != nil {
					return err
				}
			}
		}
		page, err := service.Refresh(r.Context(), session.User.ID, r.PathValue("id"), ids)
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, page)
	})
	s.Handle("GET /api/v1/source-tracks/{id}/match-candidates", true, false, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		after, limit, err := matchingPage(r, 20, 100)
		if err != nil {
			return err
		}
		page, err := service.Candidates(r.Context(), session.User.ID, r.PathValue("id"), after, limit)
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, page)
	})
	s.Handle("GET /api/v1/source-tracks/{id}/recording-link", true, false, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		link, err := service.GetLink(r.Context(), session.User.ID, r.PathValue("id"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, link)
	})
	s.Handle("GET /api/v1/recordings/{id}", true, false, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		recording, err := service.Recording(r.Context(), session.User.ID, r.PathValue("id"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, recording)
	})
	s.Handle("GET /api/v1/matching/changes", true, false, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		after, limit, err := matchingPage(r, 50, 200)
		if err != nil {
			return err
		}
		var sequence int64
		if after != "" {
			sequence, err = strconv.ParseInt(after, 10, 64)
			if err != nil || sequence < 0 {
				return domain.Fail("INVALID_ARGUMENT", "Invalid matching change sequence")
			}
		}
		page, err := service.ChangesAfter(r.Context(), session.User.ID, sequence, limit)
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, page)
	})
	s.registerMatchingMutations(service)
}

func matchingPage(r *http.Request, defaultLimit, maximum int) (string, int, error) {
	query := r.URL.Query()
	if len(query["after"]) > 1 || len(query["limit"]) > 1 || len(query.Get("after")) > 256 {
		return "", 0, domain.Fail("INVALID_ARGUMENT", "Invalid matching pagination")
	}
	limit := defaultLimit
	if values, exists := query["limit"]; exists {
		parsed, err := strconv.Atoi(values[0])
		if err != nil || parsed < 1 || parsed > maximum {
			return "", 0, domain.Fail("INVALID_ARGUMENT", "Matching limit is outside the supported range")
		}
		limit = parsed
	}
	return query.Get("after"), limit, nil
}

func matchingSourceIDs(ids []string) error {
	if len(ids) < 1 || len(ids) > matching.MaxSources {
		return domain.Fail("INVALID_ARGUMENT", "An explicit bounded source list is required")
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if id == "" || len(id) > 256 || seen[id] {
			return domain.Fail("INVALID_ARGUMENT", "Source identifiers must be nonempty and unique")
		}
		seen[id] = true
	}
	return nil
}
