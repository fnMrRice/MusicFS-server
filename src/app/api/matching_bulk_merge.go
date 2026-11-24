package api

import (
	"encoding/json"
	"net/http"

	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/metadata/matching"
)

func (s *Server) registerMatchingBulkMerge(service matchingAPI) {
	s.Handle("POST /api/v1/matching/bulk-merge", true, true, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		request, err := readBulkMergeRequest(w, r)
		if err != nil {
			return err
		}
		result, err := service.BulkMerge(r.Context(), session.User.ID, request, r.Header.Get("Idempotency-Key"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, result)
	})
}

func readBulkMergeRequest(w http.ResponseWriter, r *http.Request) (matching.BulkMergeRequest, error) {
	var input struct {
		ExpectedRevision *int64          `json:"expectedRevision"`
		Mode             string          `json:"mode"`
		MinimumScore     json.RawMessage `json:"minimumScore"`
	}
	var request matching.BulkMergeRequest
	if err := DecodeJSON(w, r, &input); err != nil {
		return request, err
	}
	if err := matchingRevision(input.ExpectedRevision); err != nil {
		return request, err
	}
	request = matching.BulkMergeRequest{ExpectedRevision: *input.ExpectedRevision, Mode: input.Mode}
	if len(input.MinimumScore) > 0 {
		var score int
		if string(input.MinimumScore) == "null" || json.Unmarshal(input.MinimumScore, &score) != nil {
			return request, domain.Fail("INVALID_ARGUMENT", "minimumScore must be an integer")
		}
		request.MinimumScore = &score
	}
	return request, request.Validate()
}
