package api

import (
	"net/http"

	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/library/playlists"
)

func (s *Server) registerPlaylistSelection(service *playlists.AggregateService) {
	s.Handle("POST /api/v1/playlists/selection", true, true, func(w http.ResponseWriter, r *http.Request, a auth.Session) error {
		var input struct {
			Name      *string                 `json:"name"`
			Selection *library.TrackSelection `json:"selection"`
		}
		if err := DecodeJSON(w, r, &input); err != nil {
			return err
		}
		if input.Name == nil || input.Selection == nil {
			return playlists.Invalid()
		}
		result, err := service.CreatePlaylistSelection(r.Context(), a.User.ID, playlists.CreateSelectionRequest{Name: *input.Name, Selection: *input.Selection}, r.Header.Get("Idempotency-Key"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusCreated, result)
	})
	s.Handle("POST /api/v1/playlists/{id}/selection", true, true, func(w http.ResponseWriter, r *http.Request, a auth.Session) error {
		var input struct {
			ExpectedRevision *int64                  `json:"expectedRevision"`
			Selection        *library.TrackSelection `json:"selection"`
		}
		if err := DecodeJSON(w, r, &input); err != nil {
			return err
		}
		if input.ExpectedRevision == nil || input.Selection == nil {
			return playlists.Invalid()
		}
		result, err := service.AppendPlaylistSelection(r.Context(), a.User.ID, r.PathValue("id"), playlists.AppendSelectionRequest{ExpectedRevision: *input.ExpectedRevision, Selection: *input.Selection}, r.Header.Get("Idempotency-Key"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, result)
	})
}
