package api

import (
	"net/http"
	"strconv"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
)

func (s *Server) RegisterLibrary(service *library.Service) {
	s.registerRecordingCovers(service)
	s.registerRootStatistics(service)
	s.Handle("GET /api/v1/source-tracks/{id}", true, false, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		// 显式来源引用不受普通音乐浏览范围影响，仍按当前账号验证所有者。
		track, err := service.Track(r.Context(), session.User.ID, r.PathValue("id"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, track)
	})
	s.Handle("GET /api/v1/source-tracks/{id}/details", true, false, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		details, err := service.Details(r.Context(), session.User.ID, r.PathValue("id"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, details)
	})
	s.Handle("GET /api/v1/provider-instances", true, false, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		return WriteJSON(w, http.StatusOK, struct {
			Items []library.Provider `json:"items"`
		}{service.Providers(r.Context(), session.User.ID)})
	})
	s.Handle("GET /api/v1/provider-instances/{id}/scan-status", true, false, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		status, err := service.ScanStatus(r.Context(), session.User.ID, r.PathValue("id"))
		if err != nil {
			return err
		}
		if status.Job != nil && status.Job.Error != nil {
			failure := *status.Job.Error
			failure.RequestID = RequestID(r)
			status.Job.Error = &failure
		}
		return WriteJSON(w, http.StatusOK, status)
	})
	s.Handle("POST /api/v1/provider-instances/{id}/scan-jobs", true, true, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		var request struct {
			Mode           string                     `json:"mode"`
			SchemaRevision string                     `json:"schemaRevision"`
			RootIDs        []string                   `json:"rootIds"`
			Options        *pluginproto.ScanOverrides `json:"options"`
		}
		if err := DecodeJSON(w, r, &request); err != nil {
			return err
		}
		if request.Mode != "full" && request.Mode != "incremental" || request.SchemaRevision == "" || len(request.RootIDs) == 0 || len(request.RootIDs) > 256 || request.Options == nil {
			return domain.Fail("INVALID_ARGUMENT", "Scan mode, schema revision, roots and options are required")
		}
		input := library.ScanInput{Mode: request.Mode, SchemaRevision: request.SchemaRevision, RootIDs: request.RootIDs, Options: request.Options.ImportOverrides, RefreshMetadata: request.Options.RefreshMetadata}
		job, merged, err := service.Scan(r.Context(), session.User.ID, r.PathValue("id"), input, r.Header.Get("Idempotency-Key"))
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
		for _, key := range []string{"groupBy", "providerInstanceId", "cursor"} {
			if len(query[key]) > 1 {
				return domain.Fail("INVALID_ARGUMENT", "Library parameters must be specified once")
			}
		}
		groupBy := query.Get("groupBy")
		if groupBy != "" && groupBy != "recording" {
			return domain.Fail("INVALID_ARGUMENT", "Unsupported library grouping")
		}
		list := service.List
		if groupBy == "recording" {
			list = service.ListGrouped
		}
		page, err := list(r.Context(), session.User.ID, query.Get("providerInstanceId"), query.Get("cursor"), limit)
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, page)
	})
}
