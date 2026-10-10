package api

import (
	"context"
	"net/http"

	"github.com/fnMrRice/MusicFS-server/src/auth"
	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/writebackhost"
)

type writeBackAPI interface {
	Submit(context.Context, string, writebackhost.Request, string) (writebackhost.Job, bool, error)
	Get(context.Context, string, string) (writebackhost.Job, error)
	Cancel(context.Context, string, string) (writebackhost.Job, error)
	Recover(context.Context, string, string) (writebackhost.Job, error)
}

func (s *Server) RegisterWriteBack(service writeBackAPI) {
	s.Handle("POST /api/v1/writeback-jobs", true, true, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		var input writeBackInput
		if err := DecodeJSON(w, r, &input); err != nil {
			return err
		}
		request, err := input.request()
		if err != nil {
			return err
		}
		job, merged, err := service.Submit(r.Context(), session.User.ID, request, r.Header.Get("Idempotency-Key"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusAccepted, struct {
			JobID  string `json:"jobId"`
			Merged bool   `json:"merged"`
		}{job.ID, merged})
	})
	s.Handle("GET /api/v1/writeback-jobs/{id}", true, false, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		job, err := service.Get(r.Context(), session.User.ID, r.PathValue("id"))
		if err != nil {
			return err
		}
		return writeBackJob(w, r, job)
	})
	for _, action := range []string{"cancel", "recover"} {
		s.Handle("POST /api/v1/writeback-jobs/{id}/"+action, true, true, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
			if err := DecodeJSON(w, r, &struct{}{}); err != nil {
				return err
			}
			var job writebackhost.Job
			var err error
			if action == "cancel" {
				job, err = service.Cancel(r.Context(), session.User.ID, r.PathValue("id"))
			} else {
				job, err = service.Recover(r.Context(), session.User.ID, r.PathValue("id"))
			}
			if err != nil {
				return err
			}
			return writeBackJob(w, r, job)
		})
	}
}

func writeBackJob(w http.ResponseWriter, r *http.Request, job writebackhost.Job) error {
	if job.Error != nil {
		failure := *job.Error
		failure.RequestID = RequestID(r)
		job.Error = &failure
	}
	return WriteJSON(w, http.StatusOK, job)
}

type writeBackInput struct {
	SourceTrackID    *string `json:"sourceTrackId"`
	AssetID          *string `json:"assetId"`
	AssetVersion     *string `json:"assetVersion"`
	ExpectedRevision *int64  `json:"expectedRevision"`
	Fields           *[]struct {
		Name     *string `json:"name"`
		Language *string `json:"language"`
	} `json:"fields"`
	Overwrite *bool `json:"overwrite"`
}

func (i writeBackInput) request() (writebackhost.Request, error) {
	r := writebackhost.Request{}
	if i.SourceTrackID == nil || i.AssetID == nil || i.AssetVersion == nil || i.ExpectedRevision == nil || *i.ExpectedRevision < 0 || i.Fields == nil || len(*i.Fields) < 1 || len(*i.Fields) > 2 || i.Overwrite == nil {
		return r, domain.Fail("INVALID_ARGUMENT", "Explicit writeback target, revision, fields and overwrite choice are required")
	}
	r.SourceTrackID, r.AssetID, r.AssetVersion, r.ExpectedRevision, r.Overwrite = *i.SourceTrackID, *i.AssetID, *i.AssetVersion, *i.ExpectedRevision, *i.Overwrite
	for _, field := range *i.Fields {
		if field.Name == nil || field.Language == nil {
			return r, domain.Fail("INVALID_ARGUMENT", "Writeback field name and language are required")
		}
		r.Fields = append(r.Fields, writebackhost.Field{Name: *field.Name, Language: *field.Language})
	}
	return r, nil
}
