package api

import (
	"net/http"

	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/plugins/dspsettings"
)

// RegisterDSPSettings 的用户身份仅来自会话，不接受请求指定其他用户。
func (s *Server) RegisterDSPSettings(service *dspsettings.Service) {
	s.Handle("GET /api/v1/dsp/settings/global", true, false, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		if session.User.Role != "admin" {
			return domain.Fail("ACCESS_DENIED", "Administrator access is required")
		}
		page, err := service.ReadGlobal(r.Context())
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, page)
	})
	s.Handle("PUT /api/v1/dsp/settings/global", true, true, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		if session.User.Role != "admin" {
			return domain.Fail("ACCESS_DENIED", "Administrator access is required")
		}
		var input dspsettings.Request
		if err := DecodeJSON(w, r, &input); err != nil {
			return err
		}
		page, err := service.UpdateGlobal(r.Context(), input, r.Header.Get("Idempotency-Key"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, page)
	})
	s.Handle("GET /api/v1/dsp/settings", true, false, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		page, err := service.Read(r.Context(), session.User.ID)
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, page)
	})
	s.Handle("PUT /api/v1/dsp/settings", true, true, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		var input dspsettings.Request
		if err := DecodeJSON(w, r, &input); err != nil {
			return err
		}
		page, err := service.Update(r.Context(), session.User.ID, input, r.Header.Get("Idempotency-Key"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, page)
	})
	s.Handle("POST /api/v1/dsp/settings/validate", true, true, func(w http.ResponseWriter, r *http.Request, _ auth.Session) error {
		var input struct {
			Settings *dspsettings.Options `json:"settings"`
		}
		if err := DecodeJSON(w, r, &input); err != nil {
			return err
		}
		if input.Settings == nil {
			return domain.Fail("INVALID_ARGUMENT", "DSP settings are required")
		}
		preview, err := service.Validate(r.Context(), *input.Settings)
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, preview)
	})
}
