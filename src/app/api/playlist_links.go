package api

import (
	"net/http"

	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/library/playlists"
)

func (s *Server) RegisterPlaylistLinks(service *playlists.LinkService) {
	s.Handle("GET /api/v1/playlist-links", true, false, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		items, err := service.List(r.Context(), session.User.ID)
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, struct {
			Items []playlists.PlaylistLink `json:"items"`
		}{items})
	})
	const route = " /api/v1/online-instances/{id}/playlists/{externalId}/link"
	s.Handle("PUT"+route, true, true, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		ref, id, err := playlistLinkInput(w, r)
		if err != nil {
			return err
		}
		link, err := service.Link(r.Context(), session.User.ID, ref, id)
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, link)
	})
	s.Handle("DELETE"+route, true, true, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		ref, id, err := playlistLinkInput(w, r)
		if err != nil {
			return err
		}
		if err = service.Unlink(r.Context(), session.User.ID, ref, id); err != nil {
			return err
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	})
}

func playlistLinkInput(w http.ResponseWriter, r *http.Request) (playlists.RemoteRef, string, error) {
	var input struct {
		AccountID  string `json:"accountId"`
		PlaylistID string `json:"playlistId"`
	}
	err := DecodeJSON(w, r, &input)
	return playlists.RemoteRef{InstanceID: r.PathValue("id"), AccountID: input.AccountID, ExternalID: r.PathValue("externalId")}, input.PlaylistID, err
}
