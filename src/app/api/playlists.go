package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/library/playlists"
)

func (s *Server) RegisterPlaylists(service *playlists.AggregateService) {
	s.Handle("POST /api/v1/playlists", true, true, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		var input playlistInput
		if err := DecodeJSON(w, r, &input); err != nil {
			return err
		}
		if input.Name == nil || input.Bindings == nil {
			return playlists.Invalid()
		}
		result, err := service.CreatePlaylist(r.Context(), session.User.ID, playlists.CreatePlaylistRequest{Name: *input.Name, Bindings: *input.Bindings}, r.Header.Get("Idempotency-Key"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusCreated, result)
	})
	s.Handle("GET /api/v1/playlists", true, false, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		page, err := remotePagination(r, false, true)
		if err != nil {
			return err
		}
		items, err := service.SearchPlaylists(r.Context(), session.User.ID, page.query, page.after, page.limit+1)
		if err != nil {
			return err
		}
		var next *string
		if len(items) > page.limit {
			items = items[:page.limit]
			next = &items[len(items)-1].ID
		}
		return WriteJSON(w, http.StatusOK, albumPage{items, next})
	})
	s.Handle("GET /api/v1/playlists/{id}", true, false, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		result, err := service.Playlist(r.Context(), session.User.ID, r.PathValue("id"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, result)
	})
	s.Handle("PUT /api/v1/playlists/{id}", true, true, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		var input struct {
			playlistInput
			Expected *int64 `json:"expectedRevision"`
		}
		if err := DecodeJSON(w, r, &input); err != nil {
			return err
		}
		if input.Name == nil || input.Bindings == nil || input.Expected == nil || *input.Expected < 1 {
			return playlists.Invalid()
		}
		result, err := service.EditPlaylist(r.Context(), session.User.ID, r.PathValue("id"), playlists.EditPlaylistRequest{ExpectedRevision: *input.Expected, Name: *input.Name, Bindings: *input.Bindings}, r.Header.Get("Idempotency-Key"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, result)
	})
	s.Handle("POST /api/v1/playlists/{id}/rebuild", true, true, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		var input struct {
			Expected *int64 `json:"expectedRuleRevision"`
		}
		if err := DecodeJSON(w, r, &input); err != nil {
			return err
		}
		if input.Expected == nil || *input.Expected < 1 {
			return playlists.Invalid()
		}
		result, err := service.RebuildPlaylist(r.Context(), session.User.ID, r.PathValue("id"), playlists.RebuildRequest{ExpectedRuleRevision: *input.Expected}, r.Header.Get("Idempotency-Key"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, playlistSnapshotReceipt{result.PlaylistID, result.Revision, result.RuleRevision, result.HideRevision, result.MatchingRevision, result.Dependencies, result.NeedsConfirmation, result.CreatedAt})
	})
	s.Handle("GET /api/v1/playlists/{id}/snapshots/{revision}", true, false, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		revision, err := remoteInteger(r.PathValue("revision"), 1, 1<<63-1)
		if err != nil {
			return err
		}
		page, err := remotePagination(r, true)
		if err != nil {
			return err
		}
		result, err := service.Page(r.Context(), session.User.ID, r.PathValue("id"), revision, page.offset, page.limit)
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, result)
	})
	s.Handle("GET /api/v1/playlists/{id}/changes", true, false, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		page, err := remotePagination(r, false)
		if err != nil {
			return err
		}
		var after int64
		if page.after != "" {
			after, err = remoteInteger(page.after, 0, 1<<63-1)
			if err != nil {
				return err
			}
		}
		items, err := service.PlaylistChanges(r.Context(), session.User.ID, r.PathValue("id"), after, page.limit+1)
		if err != nil {
			return err
		}
		var next *string
		if len(items) > page.limit {
			items = items[:page.limit]
			value := strconv.FormatInt(items[len(items)-1].Sequence, 10)
			next = &value
		}
		return WriteJSON(w, http.StatusOK, albumPage{items, next})
	})
	s.registerPlaylistHidden(service)
}

type playlistInput struct {
	Name     *string                      `json:"name"`
	Bindings *[]playlists.PlaylistBinding `json:"bindings"`
}

type playlistSnapshotReceipt struct {
	PlaylistID        string                 `json:"playlistId"`
	Revision          int64                  `json:"revision"`
	RuleRevision      int64                  `json:"ruleRevision"`
	HideRevision      int64                  `json:"hideRevision"`
	MatchingRevision  int64                  `json:"matchingRevision"`
	Dependencies      []playlists.Dependency `json:"dependencies"`
	NeedsConfirmation []string               `json:"needsConfirmation"`
	CreatedAt         time.Time              `json:"createdAt"`
}
