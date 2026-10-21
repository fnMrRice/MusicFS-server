package api

import (
	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/plugins/settings"
	"net/http"
)

// RegisterPluginSettings 只允许调用已注册插件的声明式字段和动作。
func (s *Server) RegisterPluginSettings(service *settings.Service) {
	s.Handle("GET /api/v1/plugin-settings/{pluginId}", true, false, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		page, err := service.Read(r.Context(), session.User.ID, r.PathValue("pluginId"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, page)
	})
	s.Handle("PUT /api/v1/plugin-settings/{pluginId}", true, true, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		var input settings.Request
		if err := DecodeJSON(w, r, &input); err != nil {
			return err
		}
		page, err := service.Update(r.Context(), session.User.ID, r.PathValue("pluginId"), input)
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, page)
	})
	s.Handle("POST /api/v1/plugin-settings/{pluginId}/actions/{actionId}", true, true, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		var input struct{}
		if err := DecodeJSON(w, r, &input); err != nil {
			return err
		}
		job, err := service.Execute(r.Context(), session.User.ID, r.PathValue("pluginId"), r.PathValue("actionId"), r.Header.Get("Idempotency-Key"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusAccepted, job)
	})
}
