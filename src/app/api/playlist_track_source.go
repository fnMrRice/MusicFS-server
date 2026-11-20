package api

import (
	"context"
	"net/http"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/playlists"
	"github.com/fnMrRice/MusicFS-server/src/plugins/onlinehost"
)

type playlistTrackSources interface {
	EnsurePlaylistTrackSources(context.Context, string, string, string, []string) ([]playlists.RemoteSource, error)
}

func (s *Server) RegisterPlaylistTrackSources(host *onlinehost.Service, store playlistTrackSources) {
	s.Handle("POST /api/v1/online-instances/{id}/playlist-track-sources", true, true, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		if _, err := onlineQuery(r); err != nil {
			return err
		}
		var input struct {
			AccountID   string   `json:"accountId"`
			ExternalIDs []string `json:"externalIds"`
		}
		if err := DecodeJSON(w, r, &input); err != nil {
			return err
		}
		instance := r.PathValue("id")
		if !playlists.ValidID(input.AccountID) || len(input.ExternalIDs) < 1 || len(input.ExternalIDs) > online.MaxPageItems {
			return playlists.Invalid()
		}
		for _, id := range input.ExternalIDs {
			if !online.ValidExternalID(id) {
				return playlists.Invalid()
			}
		}
		page, err := host.MetadataBatchForAccount(r.Context(), session.User.ID, instance, input.AccountID, input.ExternalIDs)
		if err != nil {
			return err
		}
		if len(page.Items) != len(input.ExternalIDs) {
			return domain.Fail("PROTOCOL_MISMATCH", "Unexpected online track count")
		}
		for i, item := range page.Items {
			if item.ExternalID != input.ExternalIDs[i] {
				return domain.Fail("PROTOCOL_MISMATCH", "Unexpected online track identity")
			}
			if item.Track == nil {
				return domain.Fail("NOT_FOUND", "Selected online track is unavailable")
			}
		}
		sources, err := store.EnsurePlaylistTrackSources(r.Context(), session.User.ID, instance, input.AccountID, input.ExternalIDs)
		if err != nil {
			return err
		}
		type reference struct {
			RemoteSourceID string `json:"remoteSourceId"`
		}
		items := make([]reference, len(sources))
		for i, source := range sources {
			items[i] = reference{source.ID}
		}
		return WriteJSON(w, http.StatusOK, struct {
			Items []reference `json:"items"`
		}{items})
	})
}
