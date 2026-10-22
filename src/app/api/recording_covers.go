package api

import (
	"encoding/json"
	"net/http"

	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
)

func (s *Server) registerRecordingCovers(service *library.Service) {
	s.Handle("GET /api/v1/recordings/{id}/cover", true, false, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		result, err := service.RecordingCover(r.Context(), session.User.ID, r.PathValue("id"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, result)
	})
	s.Handle("PUT /api/v1/recordings/{id}/cover", true, true, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		var input struct {
			ExpectedRevision *int64          `json:"expectedRevision"`
			ObjectID         json.RawMessage `json:"objectId"`
		}
		if err := DecodeJSON(w, r, &input); err != nil {
			return err
		}
		var object *string
		if input.ExpectedRevision == nil || *input.ExpectedRevision < 0 || len(input.ObjectID) == 0 || json.Unmarshal(input.ObjectID, &object) != nil {
			return domain.Fail("INVALID_ARGUMENT", "Expected revision and an explicit objectId or null are required")
		}
		result, err := service.SelectRecordingCover(r.Context(), session.User.ID, library.SelectRecordingCoverRequest{
			RecordingID: r.PathValue("id"), ExpectedRevision: *input.ExpectedRevision, ObjectID: object,
		}, r.Header.Get("Idempotency-Key"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, result)
	})
}
