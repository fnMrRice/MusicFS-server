package api

import (
	"net/http"

	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/metadata/matching"
)

func matchingRevision(revision *int64) error {
	if revision == nil || *revision < 0 {
		return domain.Fail("INVALID_ARGUMENT", "A nonnegative expectedRevision is required")
	}
	return nil
}

func (s *Server) registerMatchingMutations(service matchingAPI) {
	s.Handle("POST /api/v1/matching/links", true, true, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		var input struct {
			ExpectedRevision  *int64                  `json:"expectedRevision"`
			SourceTrackIDs    []string                `json:"sourceTrackIds"`
			Selection         *library.TrackSelection `json:"selection"`
			TargetRecordingID *string                 `json:"targetRecordingId"`
		}
		if err := DecodeJSON(w, r, &input); err != nil {
			return err
		}
		if err := matchingRevision(input.ExpectedRevision); err != nil {
			return err
		}
		if input.Selection != nil {
			if input.SourceTrackIDs != nil {
				return domain.Fail("INVALID_ARGUMENT", "Selection and sourceTrackIds are mutually exclusive")
			}
		} else if err := matchingSourceIDs(input.SourceTrackIDs); err != nil {
			return err
		}
		result, err := service.ManualLink(r.Context(), session.User.ID, matching.LinkRequest{ExpectedRevision: *input.ExpectedRevision, SourceTrackIDs: input.SourceTrackIDs, Selection: input.Selection, TargetRecordingID: input.TargetRecordingID}, r.Header.Get("Idempotency-Key"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, result)
	})
	s.Handle("POST /api/v1/matching/unlinks", true, true, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		var input struct {
			ExpectedRevision *int64 `json:"expectedRevision"`
			SourceTrackID    string `json:"sourceTrackId"`
		}
		if err := DecodeJSON(w, r, &input); err != nil {
			return err
		}
		if err := matchingRevision(input.ExpectedRevision); err != nil {
			return err
		}
		result, err := service.Unlink(r.Context(), session.User.ID, matching.UnlinkRequest{ExpectedRevision: *input.ExpectedRevision, SourceTrackID: input.SourceTrackID}, r.Header.Get("Idempotency-Key"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, result)
	})
	s.Handle("POST /api/v1/matching/splits", true, true, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		var input struct {
			ExpectedRevision *int64   `json:"expectedRevision"`
			RecordingID      string   `json:"recordingId"`
			SourceTrackIDs   []string `json:"sourceTrackIds"`
		}
		if err := DecodeJSON(w, r, &input); err != nil {
			return err
		}
		if err := matchingRevision(input.ExpectedRevision); err != nil {
			return err
		}
		if err := matchingSourceIDs(input.SourceTrackIDs); err != nil {
			return err
		}
		result, err := service.Split(r.Context(), session.User.ID, matching.SplitRequest{ExpectedRevision: *input.ExpectedRevision, RecordingID: input.RecordingID, SourceTrackIDs: input.SourceTrackIDs}, r.Header.Get("Idempotency-Key"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, result)
	})
	s.Handle("POST /api/v1/matching/exclusions", true, true, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		var input struct {
			ExpectedRevision   *int64 `json:"expectedRevision"`
			LeftSourceTrackID  string `json:"leftSourceTrackId"`
			RightSourceTrackID string `json:"rightSourceTrackId"`
			Excluded           *bool  `json:"excluded"`
		}
		if err := DecodeJSON(w, r, &input); err != nil {
			return err
		}
		if err := matchingRevision(input.ExpectedRevision); err != nil {
			return err
		}
		if input.Excluded == nil {
			return domain.Fail("INVALID_ARGUMENT", "An explicit excluded decision is required")
		}
		result, err := service.SetExclusion(r.Context(), session.User.ID, matching.ExclusionRequest{ExpectedRevision: *input.ExpectedRevision, LeftSourceTrackID: input.LeftSourceTrackID, RightSourceTrackID: input.RightSourceTrackID, Excluded: *input.Excluded}, r.Header.Get("Idempotency-Key"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, result)
	})
}
