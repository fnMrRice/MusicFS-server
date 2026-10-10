package api

import (
	"net/http"

	"github.com/fnMrRice/MusicFS-server/src/auth"
	"github.com/fnMrRice/MusicFS-server/src/identification"
	"github.com/fnMrRice/MusicFS-server/src/identification/hashworker"
)

func (s *Server) RegisterHashing(service *hashworker.Service) {
	s.Handle("POST /api/v1/source-tracks/{id}/hash-jobs", true, true, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		if err := DecodeJSON(w, r, &struct{}{}); err != nil {
			return err
		}
		job, merged, err := service.Submit(r.Context(), session.User.ID, r.PathValue("id"), r.Header.Get("Idempotency-Key"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusAccepted, struct {
			JobID  string `json:"jobId"`
			Merged bool   `json:"merged"`
		}{job.ID, merged})
	})
	s.Handle("GET /api/v1/hash-jobs/{id}", true, false, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		job, err := service.Get(r.Context(), session.User.ID, r.PathValue("id"))
		if err != nil {
			return err
		}
		return writeHashJob(w, r, job)
	})
	s.Handle("POST /api/v1/hash-jobs/{id}/cancel", true, true, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		if err := DecodeJSON(w, r, &struct{}{}); err != nil {
			return err
		}
		job, err := service.Cancel(r.Context(), session.User.ID, r.PathValue("id"))
		if err != nil {
			return err
		}
		return writeHashJob(w, r, job)
	})
}

func writeHashJob(w http.ResponseWriter, r *http.Request, job identification.HashJob) error {
	if job.Error != nil {
		failure := *job.Error
		failure.RequestID = RequestID(r)
		job.Error = &failure
	}
	return WriteJSON(w, http.StatusOK, job)
}
