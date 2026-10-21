package api

import (
	"net/http"

	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/library/playlists"
)

func (s *Server) RegisterRemotePlaylists(service *playlists.Service) {
	s.Handle("POST /api/v1/remote-playlists", true, true, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		var input struct {
			InstanceID *string `json:"instanceId"`
			AccountID  *string `json:"accountId"`
			ExternalID *string `json:"externalId"`
		}
		if err := DecodeJSON(w, r, &input); err != nil {
			return err
		}
		if input.InstanceID == nil || input.AccountID == nil || input.ExternalID == nil {
			return playlists.Invalid()
		}
		item, merged, err := service.Import(r.Context(), session.User.ID, playlists.RemoteRef{InstanceID: *input.InstanceID, AccountID: *input.AccountID, ExternalID: *input.ExternalID}, r.Header.Get("Idempotency-Key"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusCreated, struct {
			Playlist playlists.RemotePlaylist `json:"playlist"`
			Merged   bool                     `json:"merged"`
		}{remotePlaylistResponse(r, item), merged})
	})
	s.Handle("GET /api/v1/remote-playlists", true, false, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		page, err := remotePagination(r, false)
		if err != nil {
			return err
		}
		items, err := service.List(r.Context(), session.User.ID, page.after, page.limit+1)
		if err != nil {
			return err
		}
		var next *string
		if len(items) > page.limit {
			items = items[:page.limit]
			next = &items[len(items)-1].ID
		}
		for i := range items {
			items[i] = remotePlaylistResponse(r, items[i])
		}
		return WriteJSON(w, http.StatusOK, albumPage{items, next})
	})
	s.Handle("GET /api/v1/remote-playlists/{id}", true, false, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		item, err := service.Get(r.Context(), session.User.ID, r.PathValue("id"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, remotePlaylistResponse(r, item))
	})
	s.Handle("POST /api/v1/remote-playlists/{id}/sync-jobs", true, true, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		var input struct {
			Expected *int64 `json:"expectedBindingRevision"`
		}
		if err := DecodeJSON(w, r, &input); err != nil {
			return err
		}
		if input.Expected == nil || *input.Expected < 1 {
			return playlists.Invalid()
		}
		job, merged, err := service.Sync(r.Context(), session.User.ID, r.PathValue("id"), *input.Expected, r.Header.Get("Idempotency-Key"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusAccepted, struct {
			JobID  string `json:"jobId"`
			Merged bool   `json:"merged"`
		}{job.ID, merged})
	})
	s.Handle("GET /api/v1/remote-playlist-sync-jobs/{id}", true, false, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		job, err := service.GetJob(r.Context(), session.User.ID, r.PathValue("id"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, remoteJobResponse(r, job))
	})
	s.Handle("POST /api/v1/remote-playlist-sync-jobs/{id}/cancel", true, true, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		if err := DecodeJSON(w, r, &struct{}{}); err != nil {
			return err
		}
		job, err := service.Cancel(r.Context(), session.User.ID, r.PathValue("id"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, remoteJobResponse(r, job))
	})
	s.Handle("GET /api/v1/remote-playlists/{id}/snapshots/{revision}", true, false, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		revision, err := remoteInteger(r.PathValue("revision"), 1, 1<<63-1)
		if err != nil {
			return err
		}
		page, err := remotePagination(r, true)
		if err != nil {
			return err
		}
		result, err := service.Snapshot(r.Context(), session.User.ID, r.PathValue("id"), revision, page.offset, page.limit)
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, result)
	})
}

func remotePlaylistResponse(r *http.Request, item playlists.RemotePlaylist) playlists.RemotePlaylist {
	if item.Error != nil {
		failure := *item.Error
		failure.RequestID = RequestID(r)
		item.Error = &failure
	}
	return item
}

func remoteJobResponse(r *http.Request, job playlists.SyncJob) playlists.SyncJob {
	if job.Error != nil {
		failure := *job.Error
		failure.RequestID = RequestID(r)
		job.Error = &failure
	}
	return job
}
