package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/fnMrRice/MusicFS-server/src/auth"
	"github.com/fnMrRice/MusicFS-server/src/config"
	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/plugincontrol"
)

func requireAdmin(next Handler) Handler {
	return func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		if session.User.Role != "admin" {
			return domain.Fail("ACCESS_DENIED", "Administrator role is required")
		}
		return next(w, r, session)
	}
}

func (s *Server) RegisterPluginManagement(manager *plugincontrol.Manager) {
	s.registerPluginDirectories(manager)
	s.Handle("GET /api/v1/admin/plugins", true, false, requireAdmin(func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		if _, err := onlineQuery(r); err != nil {
			return err
		}
		v := manager.SnapshotFor(r.Context(), session.User.ID)
		w.Header().Set("ETag", `"`+v.Revision+`"`)
		return WriteJSON(w, 200, v)
	}))
	s.Handle("GET /api/v1/admin/plugins/{pluginId}/instances", true, false, requireAdmin(func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		if _, err := onlineQuery(r); err != nil {
			return err
		}
		v := manager.SnapshotFor(r.Context(), session.User.ID)
		for _, p := range v.Items {
			if p.PluginID == r.PathValue("pluginId") {
				return WriteJSON(w, 200, struct {
					Revision string                   `json:"revision"`
					Items    []plugincontrol.Instance `json:"items"`
				}{v.Revision, p.Instances})
			}
		}
		return domain.Fail("NOT_FOUND", "Plugin not found")
	}))
	for _, route := range []string{"PUT /api/v1/admin/plugins/{pluginId}/state", "PUT /api/v1/admin/plugin-instances/{instanceId}/state"} {
		s.Handle(route, true, true, requireAdmin(func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
			if _, err := onlineQuery(r); err != nil {
				return err
			}
			var input struct {
				Enabled *bool  `json:"enabled"`
				Mode    string `json:"mode,omitempty"`
			}
			if err := DecodeJSON(w, r, &input); err != nil {
				return err
			}
			if input.Enabled == nil {
				return domain.Fail("INVALID_ARGUMENT", "enabled is required")
			}
			plugin := r.PathValue("pluginId")
			instance := r.PathValue("instanceId")
			if instance != "" {
				p, _, err := manager.InstanceConfig(instance)
				if err != nil {
					return err
				}
				plugin = p.PluginID
			}
			op, err := manager.Change(r.Context(), session.User.ID, config.Change{ExpectedRevision: strings.Trim(r.Header.Get("If-Match"), `"`), IdempotencyKey: r.Header.Get("Idempotency-Key"), PluginID: plugin, InstanceID: instance, Enabled: *input.Enabled, Mode: input.Mode})
			if err != nil {
				return err
			}
			return WriteJSON(w, http.StatusAccepted, op)
		}))
	}
	s.Handle("GET /api/v1/admin/plugin-operations/{id}", true, false, requireAdmin(func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		if _, err := onlineQuery(r); err != nil {
			return err
		}
		op, err := manager.Operation(r.Context(), session.User.ID, r.PathValue("id"))
		if err != nil {
			return err
		}
		return WriteJSON(w, 200, op)
	}))
	s.Handle("GET /api/v1/admin/plugin-instances/{id}/configuration", true, false, requireAdmin(func(w http.ResponseWriter, r *http.Request, _ auth.Session) error {
		if _, err := onlineQuery(r); err != nil {
			return err
		}
		v, err := manager.Configuration(r.PathValue("id"))
		if err != nil {
			return err
		}
		w.Header().Set("ETag", `"`+v.Revision+`"`)
		return WriteJSON(w, 200, v)
	}))
	s.Handle("PUT /api/v1/admin/plugin-instances/{id}/configuration", true, true, requireAdmin(func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		if _, err := onlineQuery(r); err != nil {
			return err
		}
		var raw json.RawMessage
		if err := DecodeJSON(w, r, &raw); err != nil {
			return err
		}
		p, _, err := manager.InstanceConfig(r.PathValue("id"))
		if err != nil {
			return err
		}
		input, err := plugincontrol.ValidateSettings(p.PluginID, raw)
		if err != nil {
			return err
		}
		op, err := manager.Change(r.Context(), session.User.ID, config.Change{ExpectedRevision: strings.Trim(r.Header.Get("If-Match"), `"`), IdempotencyKey: r.Header.Get("Idempotency-Key"), PluginID: p.PluginID, InstanceID: r.PathValue("id"), Mode: "drain", Settings: &input})
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusAccepted, op)
	}))
	s.registerPluginEvents(manager)
}
