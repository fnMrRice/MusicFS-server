package api

import (
	"encoding/json"
	"net/http"

	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/playback/downloadjobs"
)

func (s *Server) RegisterOnlineDownloadJobs(service *downloadjobs.Service) {
	service.OnChange(s.notifyDownload)
	const maxBody = 2 << 20
	s.registerBounded("POST /api/v1/online-instances/{id}/download-jobs", true, true, s.options.ControlTimeout, maxBody, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		if _, err := onlineQuery(r); err != nil {
			return err
		}
		var input struct {
			ExternalIDs     []string        `json:"externalIds"`
			Quality         *string         `json:"quality"`
			AccountID       *string         `json:"accountId"`
			AccountRevision json.RawMessage `json:"accountRevision,omitempty"`
		}
		if err := decodeJSONLimit(w, r, &input, maxBody); err != nil {
			return err
		}
		if input.AccountID == nil || input.Quality == nil {
			return domain.Fail("INVALID_ARGUMENT", "Download quality and account ID are required")
		}
		var revision string
		if input.AccountRevision != nil && (json.Unmarshal(input.AccountRevision, &revision) != nil || revision == "") {
			return domain.Fail("INVALID_ARGUMENT", "Invalid account revision")
		}
		job, err := service.Submit(r.Context(), session.User.ID, r.PathValue("id"), r.Header.Get("Idempotency-Key"), downloadjobs.Request{ExternalIDs: input.ExternalIDs, Quality: *input.Quality, AccountID: *input.AccountID, AccountRevision: revision})
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusAccepted, job)
	})
	s.Handle("GET /api/v1/online-download-jobs/{jobId}", true, false, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		query, err := onlineQuery(r, "view")
		if err != nil {
			return err
		}
		id := r.PathValue("jobId")
		if query.Has("view") {
			if query.Get("view") != "summary" {
				return domain.Fail("INVALID_ARGUMENT", "Unsupported online download job view")
			}
			summary, err := service.Summary(session.User.ID, id)
			if err != nil {
				return err
			}
			return WriteJSON(w, http.StatusOK, summary)
		}
		job, err := service.Get(session.User.ID, id)
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, job)
	})
}
