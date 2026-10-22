package api

import (
	"net/http"

	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/playback"
	"github.com/fnMrRice/MusicFS-server/src/playback/timeline"
)

func (s *Server) registerTimeline(service *playback.Service) {
	s.Handle("GET /api/v1/playback/recovery-profile", true, false, func(w http.ResponseWriter, r *http.Request, a auth.Session) error {
		profile, err := service.RecoveryProfileForUser(r.Context(), a.User.ID)
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, profile)
	})
	s.Handle("POST /api/v1/timeline-evidence", true, true, func(w http.ResponseWriter, r *http.Request, a auth.Session) error {
		request, err := decodeTimeline(w, r)
		if err != nil {
			return err
		}
		result, err := service.AddTimeline(r.Context(), a.User.ID, request, r.Header.Get("Idempotency-Key"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusCreated, result)
	})
	s.Handle("GET /api/v1/timeline-evidence/{id}", true, false, func(w http.ResponseWriter, r *http.Request, a auth.Session) error {
		result, err := service.Timeline(r.Context(), a.User.ID, r.PathValue("id"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, result)
	})
	s.Handle("POST /api/v1/timeline-evidence/{id}/revoke", true, true, func(w http.ResponseWriter, r *http.Request, a auth.Session) error {
		var request struct {
			ExpectedRevision *int64 `json:"expectedRevision"`
		}
		if err := DecodeJSON(w, r, &request); err != nil {
			return err
		}
		if request.ExpectedRevision == nil {
			return domain.Fail("INVALID_ARGUMENT", "expectedRevision is required")
		}
		result, err := service.RevokeTimeline(r.Context(), a.User.ID, r.PathValue("id"), *request.ExpectedRevision, r.Header.Get("Idempotency-Key"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, result)
	})
	s.Handle("POST /api/v1/playback/sessions/{id}/recover-source", true, true, func(w http.ResponseWriter, r *http.Request, a auth.Session) error {
		var wire struct {
			AttemptID           string  `json:"attemptId"`
			TargetSourceTrackID string  `json:"targetSourceTrackId"`
			PositionMs          *int64  `json:"positionMs"`
			EvidenceID          *string `json:"evidenceId"`
			ObservedError       *struct {
				Code          *string                 `json:"code"`
				Message       *string                 `json:"message"`
				OutputProfile *playback.OutputProfile `json:"outputProfile,omitempty"`
			} `json:"observedError"`
		}
		if err := DecodeJSON(w, r, &wire); err != nil {
			return err
		}
		if wire.PositionMs == nil {
			return domain.Fail("INVALID_ARGUMENT", "positionMs is required")
		}
		if wire.ObservedError == nil || wire.ObservedError.Code == nil || wire.ObservedError.Message == nil {
			return domain.Fail("INVALID_ARGUMENT", "observedError code and message are required")
		}
		observed := &playback.ObservedError{Code: *wire.ObservedError.Code, Message: *wire.ObservedError.Message, OutputProfile: wire.ObservedError.OutputProfile}
		request := playback.RecoverSourceRequest{AttemptID: wire.AttemptID, TargetSourceTrackID: wire.TargetSourceTrackID, PositionMs: *wire.PositionMs, EvidenceID: wire.EvidenceID, ObservedError: observed}
		result, err := service.RecoverSource(r.Context(), a, r.PathValue("id"), request, r.Header.Get("Idempotency-Key"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, result)
	})
}

func decodeTimeline(w http.ResponseWriter, r *http.Request) (timeline.Request, error) {
	var wire struct {
		RecordingID string           `json:"recordingId"`
		From        *timeline.Target `json:"from"`
		To          *timeline.Target `json:"to"`
		Range       *struct {
			StartMs *int64 `json:"startMs"`
			EndMs   *int64 `json:"endMs"`
		} `json:"range"`
		OffsetMs *int64 `json:"offsetMs"`
		Profile  *struct {
			OutputProfile      string   `json:"outputProfile"`
			ProcessingRevision string   `json:"processingRevision"`
			DecoderSHA256      string   `json:"decoderSha256"`
			GainDB             *float64 `json:"gainDb"`
		} `json:"profile"`
	}
	var result timeline.Request
	if err := DecodeJSON(w, r, &wire); err != nil {
		return result, err
	}
	if wire.From == nil || wire.To == nil || wire.Range == nil || wire.Range.StartMs == nil || wire.Range.EndMs == nil || wire.OffsetMs == nil || wire.Profile == nil || wire.Profile.GainDB == nil {
		return result, domain.Fail("INVALID_ARGUMENT", "All alignment targets, coverage, offset and processing fields are required")
	}
	result = timeline.Request{RecordingID: wire.RecordingID, From: *wire.From, To: *wire.To, Range: timeline.Range{StartMs: *wire.Range.StartMs, EndMs: *wire.Range.EndMs}, OffsetMs: *wire.OffsetMs, Profile: timeline.Profile{OutputProfile: wire.Profile.OutputProfile, ProcessingRevision: wire.Profile.ProcessingRevision, DecoderSHA256: wire.Profile.DecoderSHA256, GainDB: *wire.Profile.GainDB}}
	return result, result.Validate()
}
