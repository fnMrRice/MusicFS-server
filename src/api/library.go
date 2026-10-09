package api

import (
	"net/http"
	"strconv"

	"github.com/fnMrRice/MusicFS-server/src/auth"
	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
)

func (s *Server) RegisterLibrary(service *library.Service) {
	s.Handle("GET /api/v1/provider-instances", true, false, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		return WriteJSON(w, http.StatusOK, struct {
			Items []library.Provider `json:"items"`
		}{service.Providers(r.Context(), session.User.ID)})
	})
	s.Handle("POST /api/v1/provider-instances/{id}/scan-jobs", true, true, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		var request struct {
			Mode string `json:"mode"`
		}
		if err := DecodeJSON(w, r, &request); err != nil {
			return err
		}
		if request.Mode != "full" && request.Mode != "incremental" {
			return domain.Fail("INVALID_ARGUMENT", "Invalid scan mode")
		}
		job, merged, err := service.Scan(r.Context(), session.User.ID, r.PathValue("id"), request.Mode, r.Header.Get("Idempotency-Key"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusAccepted, struct {
			JobID  string `json:"jobId"`
			Merged bool   `json:"merged"`
		}{job.ID, merged})
	})
	s.Handle("GET /api/v1/scan-jobs/{id}", true, false, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		job, err := service.Job(r.Context(), session.User.ID, r.PathValue("id"))
		if err != nil {
			return err
		}
		if job.Error != nil {
			failure := *job.Error
			failure.RequestID = RequestID(r)
			job.Error = &failure
		}
		return WriteJSON(w, http.StatusOK, job)
	})
	s.Handle("GET /api/v1/source-tracks", true, false, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		query := r.URL.Query()
		limit := 50
		if values, exists := query["limit"]; exists {
			if len(values) != 1 {
				return domain.Fail("INVALID_ARGUMENT", "limit must be specified once")
			}
			var err error
			limit, err = strconv.Atoi(values[0])
			if err != nil || limit < 1 || limit > 200 {
				return domain.Fail("INVALID_ARGUMENT", "limit must be between 1 and 200")
			}
		}
		page, err := service.List(r.Context(), session.User.ID, query.Get("providerInstanceId"), query.Get("cursor"), limit)
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, page)
	})
}
