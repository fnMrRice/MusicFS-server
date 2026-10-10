package api

import (
	"encoding/json"
	"net/http"

	"github.com/fnMrRice/MusicFS-server/src/auth"
	"github.com/fnMrRice/MusicFS-server/src/playlists"
)

func (s *Server) registerPlaylistHidden(service *playlists.AggregateService) {
	s.Handle("GET /api/v1/playlists/{id}/hidden-rules", true, false, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		result, err := service.HiddenRules(r.Context(), session.User.ID, r.PathValue("id"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, result)
	})
	s.Handle("POST /api/v1/playlists/{id}/hidden-rules", true, true, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		var input struct {
			Expected    *int64              `json:"expectedRevision"`
			Scope       json.RawMessage     `json:"scope"`
			Source      playlists.SourceRef `json:"source"`
			BindingPath []string            `json:"bindingPath"`
			EntryID     *string             `json:"entryId"`
		}
		if err := DecodeJSON(w, r, &input); err != nil {
			return err
		}
		if input.Expected == nil || *input.Expected < 0 {
			return playlists.Invalid()
		}
		scope := "allSources"
		if len(input.Scope) > 0 {
			var provided *string
			if json.Unmarshal(input.Scope, &provided) != nil || provided == nil {
				return playlists.Invalid()
			}
			scope = *provided
		}
		result, err := service.HidePlaylistSource(r.Context(), session.User.ID, r.PathValue("id"), playlists.HideRequest{ExpectedRevision: *input.Expected, Scope: scope, Source: input.Source, BindingPath: input.BindingPath, EntryID: input.EntryID}, r.Header.Get("Idempotency-Key"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, result)
	})
	s.Handle("PUT /api/v1/playlists/{id}/hidden-rules/{ruleId}", true, true, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		var input struct {
			Expected *int64 `json:"expectedRevision"`
			Active   *bool  `json:"active"`
			Confirm  *bool  `json:"confirm"`
		}
		if err := DecodeJSON(w, r, &input); err != nil {
			return err
		}
		if input.Expected == nil || *input.Expected < 0 || input.Active == nil || input.Confirm == nil {
			return playlists.Invalid()
		}
		result, err := service.ChangePlaylistHidden(r.Context(), session.User.ID, r.PathValue("id"), r.PathValue("ruleId"), playlists.ChangeHiddenRequest{ExpectedRevision: *input.Expected, Active: *input.Active, Confirm: *input.Confirm}, r.Header.Get("Idempotency-Key"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, result)
	})
}
