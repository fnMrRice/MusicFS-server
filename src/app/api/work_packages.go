package api

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/workpackages"
)

func (s *Server) RegisterWorkPackages(service *workpackages.Service) {
	s.Handle("GET /api/v1/works/{id}/package-sources", true, false, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		if r.URL.RawQuery != "" {
			return invalidWorkInput()
		}
		items, err := service.Sources(r.Context(), session.User.ID, r.PathValue("id"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, map[string]any{"items": items})
	})
	s.Handle("GET /api/v1/works/{id}/package-directories", true, false, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		input, err := packageSelection(r.URL.RawQuery)
		if err != nil {
			return err
		}
		result, err := service.Browse(r.Context(), session.User.ID, r.PathValue("id"), input)
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, result)
	})
	s.Handle("GET /api/v1/works/{id}/packages", true, false, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		if r.URL.RawQuery != "" {
			return invalidWorkInput()
		}
		items, err := service.List(r.Context(), session.User.ID, r.PathValue("id"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, map[string]any{"items": items})
	})
	s.packageMutation("POST /api/v1/works/{id}/package-previews", func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		var input workpackages.Selection
		if err := DecodeJSON(w, r, &input); err != nil {
			return err
		}
		result, err := service.Preview(r.Context(), session.User.ID, r.PathValue("id"), input)
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, result)
	})
	s.packageMutation("POST /api/v1/works/{id}/packages", func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		var input struct {
			PreviewID string `json:"previewId"`
		}
		if err := DecodeJSON(w, r, &input); err != nil {
			return err
		}
		result, err := service.Import(r.Context(), session.User.ID, r.PathValue("id"), input.PreviewID, r.Header.Get("Idempotency-Key"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, result)
	})
	s.HandleMediaDelivery("GET /api/v1/works/{id}/packages/{packageId}/attachments/{nodeId}", func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		return deliverWorkAttachment(w, r, session.User.ID, service)
	})
}

// packageMutation 允许有界的目录枚举与音频探测，仍保留鉴权、CSRF、排空和正文限制。
func (s *Server) packageMutation(pattern string, handler Handler) {
	s.register(pattern, true, true, true, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		if r.URL.RawQuery != "" {
			return invalidWorkInput()
		}
		ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
		defer cancel()
		controller := http.NewResponseController(w)
		deadline, _ := ctx.Deadline()
		for _, set := range []func(time.Time) error{controller.SetReadDeadline, controller.SetWriteDeadline} {
			if err := set(deadline); err != nil && !errors.Is(err, http.ErrNotSupported) {
				return domain.Fail("TEMPORARY_FAILURE", "Package operation deadline could not be set")
			}
			defer set(time.Time{})
		}
		r.Body = http.MaxBytesReader(w, r.Body, MaxRequestBytes)
		return handler(w, r.WithContext(ctx), session)
	})
}

func packageSelection(raw string) (workpackages.Selection, error) {
	values, err := url.ParseQuery(raw)
	if err != nil || len(values) != 3 {
		return workpackages.Selection{}, invalidWorkInput()
	}
	for _, key := range []string{"providerInstanceId", "rootId", "path"} {
		if len(values[key]) != 1 || values[key][0] == "" {
			return workpackages.Selection{}, invalidWorkInput()
		}
	}
	return workpackages.Selection{ProviderID: values.Get("providerInstanceId"), RootID: values.Get("rootId"), Path: values.Get("path")}, nil
}
