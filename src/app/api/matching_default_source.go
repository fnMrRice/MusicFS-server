package api

import (
	"encoding/json"
	"net/http"

	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/metadata/matching"
)

func (s *Server) registerMatchingDefaultSource(service matchingAPI) {
	s.Handle("POST /api/v1/matching/default-source", true, true, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		var input struct {
			ExpectedRevision *int64          `json:"expectedRevision"`
			RecordingID      string          `json:"recordingId"`
			SourceTrackID    json.RawMessage `json:"sourceTrackId"`
		}
		if err := DecodeJSON(w, r, &input); err != nil {
			return err
		}
		if err := matchingRevision(input.ExpectedRevision); err != nil {
			return err
		}
		var source *string
		if len(input.SourceTrackID) == 0 || json.Unmarshal(input.SourceTrackID, &source) != nil {
			return domain.Fail("INVALID_ARGUMENT", "An explicit sourceTrackId or null is required")
		}
		result, err := service.SetDefaultSource(r.Context(), session.User.ID, matching.DefaultSourceRequest{
			ExpectedRevision: *input.ExpectedRevision, RecordingID: input.RecordingID, SourceTrackID: source,
		}, r.Header.Get("Idempotency-Key"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, result)
	})
}
