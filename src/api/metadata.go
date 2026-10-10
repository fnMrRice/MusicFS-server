package api

import (
	"context"
	"net/http"

	"github.com/fnMrRice/MusicFS-server/src/auth"
	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/enrichment"
	"github.com/fnMrRice/MusicFS-server/src/mediastore"
)

type enrichmentAPI interface {
	Instances(context.Context, string) []enrichment.Instance
	Submit(context.Context, string, enrichment.JobRequest, string) (enrichment.Job, bool, error)
	Get(context.Context, string, string) (enrichment.Job, error)
	Cancel(context.Context, string, string) (enrichment.Job, error)
	Apply(context.Context, string, string, int64, string) (mediastore.Selection, error)
}

func (s *Server) RegisterEnrichment(service enrichmentAPI) {
	s.Handle("GET /api/v1/scraper-instances", true, false, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		return WriteJSON(w, http.StatusOK, struct {
			Items []enrichment.Instance `json:"items"`
		}{service.Instances(r.Context(), session.User.ID)})
	})
	s.Handle("POST /api/v1/metadata-jobs", true, true, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		var input metadataJobInput
		if err := DecodeJSON(w, r, &input); err != nil {
			return err
		}
		request := enrichment.JobRequest{SourceTrackIDs: input.SourceTrackIDs, Fields: input.Fields, Languages: input.Languages, InstanceIDs: input.InstanceIDs, ExcludeInstanceIDs: input.ExcludeInstanceIDs}
		var err error
		request.SourceAlbum, err = input.album()
		if err != nil {
			return err
		}
		if input.Query != nil {
			request.Query = &enrichment.Query{Title: input.Query.Title, Artists: input.Query.Artists}
		}
		job, merged, err := service.Submit(r.Context(), session.User.ID, request, r.Header.Get("Idempotency-Key"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusAccepted, struct {
			JobID  string `json:"jobId"`
			Merged bool   `json:"merged"`
		}{job.JobID, merged})
	})
	s.Handle("GET /api/v1/metadata-jobs/{id}", true, false, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		job, err := service.Get(r.Context(), session.User.ID, r.PathValue("id"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, job)
	})
	s.Handle("POST /api/v1/metadata-jobs/{id}/cancel", true, true, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		if err := DecodeJSON(w, r, &struct{}{}); err != nil {
			return err
		}
		job, err := service.Cancel(r.Context(), session.User.ID, r.PathValue("id"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, job)
	})
	s.Handle("POST /api/v1/metadata-candidates/{id}/apply", true, true, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		var input struct {
			ExpectedRevision *int64 `json:"expectedRevision"`
		}
		if err := DecodeJSON(w, r, &input); err != nil {
			return err
		}
		if input.ExpectedRevision == nil || *input.ExpectedRevision < 0 {
			return domain.Fail("INVALID_ARGUMENT", "Expected media revision is required")
		}
		selection, err := service.Apply(r.Context(), session.User.ID, r.PathValue("id"), *input.ExpectedRevision, r.Header.Get("Idempotency-Key"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, selection)
	})
}
