package api

import (
	"net/http"
	"strconv"

	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/metadata/identification"
	"github.com/fnMrRice/MusicFS-server/src/metadata/identification/fingerprintworker"
)

func fingerprintingUnavailable(service *fingerprintworker.Service) error {
	if service == nil {
		return domain.Fail("UNSUPPORTED", "Fingerprint extraction is not configured or available")
	}
	return nil
}

func (s *Server) RegisterFingerprinting(service *fingerprintworker.Service) {
	s.Handle("POST /api/v1/source-tracks/{id}/fingerprint-jobs", true, true, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		var request struct {
			StartMs    *int64 `json:"startMs"`
			DurationMs *int64 `json:"durationMs"`
		}
		if err := DecodeJSON(w, r, &request); err != nil {
			return err
		}
		if request.StartMs == nil || request.DurationMs == nil || *request.StartMs < 0 || *request.StartMs > 86400000 || *request.DurationMs < 10000 || *request.DurationMs > 120000 {
			return domain.Fail("INVALID_ARGUMENT", "Fingerprint startMs and durationMs are required within the supported range")
		}
		if err := fingerprintingUnavailable(service); err != nil {
			return err
		}
		job, merged, err := service.Submit(r.Context(), session.User.ID, r.PathValue("id"), r.Header.Get("Idempotency-Key"), *request.StartMs, *request.DurationMs)
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusAccepted, struct {
			JobID  string `json:"jobId"`
			Merged bool   `json:"merged"`
		}{job.ID, merged})
	})
	s.Handle("GET /api/v1/fingerprint-jobs/{id}", true, false, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		if err := fingerprintingUnavailable(service); err != nil {
			return err
		}
		job, err := service.Get(r.Context(), session.User.ID, r.PathValue("id"))
		if err != nil {
			return err
		}
		return writeFingerprintJob(w, r, job)
	})
	s.Handle("POST /api/v1/fingerprint-jobs/{id}/cancel", true, true, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		if err := DecodeJSON(w, r, &struct{}{}); err != nil {
			return err
		}
		if err := fingerprintingUnavailable(service); err != nil {
			return err
		}
		job, err := service.Cancel(r.Context(), session.User.ID, r.PathValue("id"))
		if err != nil {
			return err
		}
		return writeFingerprintJob(w, r, job)
	})
	s.Handle("GET /api/v1/source-tracks/{id}/fingerprints", true, false, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		query := r.URL.Query()
		limit := 50
		if len(query["after"]) > 1 || len(query["limit"]) > 1 || len(query.Get("after")) > 256 {
			return domain.Fail("INVALID_ARGUMENT", "Invalid fingerprint pagination")
		}
		if values, exists := query["limit"]; exists {
			value, err := strconv.Atoi(values[0])
			if err != nil || value < 1 || value > 100 {
				return domain.Fail("INVALID_ARGUMENT", "Fingerprint limit must be between 1 and 100")
			}
			limit = value
		}
		if err := fingerprintingUnavailable(service); err != nil {
			return err
		}
		items, err := service.Indexes(r.Context(), session.User.ID, r.PathValue("id"), query.Get("after"), limit+1)
		if err != nil {
			return err
		}
		if items == nil {
			items = []identification.FingerprintIndex{}
		}
		var next *string
		if len(items) > limit {
			value := items[limit-1].ID
			next = &value
			items = items[:limit]
		}
		return WriteJSON(w, http.StatusOK, struct {
			Items     []identification.FingerprintIndex `json:"items"`
			NextAfter *string                           `json:"nextAfter"`
		}{items, next})
	})
	s.Handle("GET /api/v1/fingerprints/{id}", true, false, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		if err := fingerprintingUnavailable(service); err != nil {
			return err
		}
		detail, err := service.Detail(r.Context(), session.User.ID, r.PathValue("id"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, detail)
	})
}

func writeFingerprintJob(w http.ResponseWriter, r *http.Request, job identification.FingerprintJob) error {
	if job.Error != nil {
		detail := *job.Error
		detail.RequestID = RequestID(r)
		job.Error = &detail
	}
	return WriteJSON(w, http.StatusOK, job)
}
