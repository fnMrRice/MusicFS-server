package api

import (
	"net/http"
	"strconv"

	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/albums"
)

func (s *Server) RegisterAlbums(service *albums.Service) {
	s.Handle("GET /api/v1/source-albums", true, false, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		page, err := albumPagination(r, true, false)
		if err != nil {
			return err
		}
		items, err := service.SourceAlbums(r.Context(), session.User.ID, page.provider, page.after, page.limit+1)
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
	s.Handle("POST /api/v1/source-albums", true, true, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		var input sourceAlbumInput
		if err := DecodeJSON(w, r, &input); err != nil {
			return err
		}
		if input.Provider == nil || input.Name == nil || input.Members == nil {
			return invalidAlbumInput()
		}
		album, err := service.CreateSourceAlbum(r.Context(), session.User.ID, albums.CreateSourceAlbumRequest{ProviderInstanceID: *input.Provider, Name: *input.Name, SourceTrackIDs: *input.Members}, r.Header.Get("Idempotency-Key"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusCreated, album)
	})
	s.Handle("GET /api/v1/source-albums/{id}", true, false, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		album, err := service.SourceAlbum(r.Context(), session.User.ID, r.PathValue("id"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, album)
	})
	s.Handle("PUT /api/v1/source-albums/{id}", true, true, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		var input struct {
			ExpectedRevision *int64    `json:"expectedRevision"`
			Name             *string   `json:"name"`
			Members          *[]string `json:"sourceTrackIds"`
		}
		if err := DecodeJSON(w, r, &input); err != nil {
			return err
		}
		if input.ExpectedRevision == nil || *input.ExpectedRevision < 1 || input.Name == nil || input.Members == nil {
			return invalidAlbumInput()
		}
		album, err := service.UpdateSourceAlbum(r.Context(), session.User.ID, r.PathValue("id"), albums.UpdateSourceAlbumRequest{ExpectedRevision: *input.ExpectedRevision, Name: *input.Name, SourceTrackIDs: *input.Members}, r.Header.Get("Idempotency-Key"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, album)
	})
	s.Handle("GET /api/v1/releases", true, false, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		page, err := albumPagination(r, false, false)
		if err != nil {
			return err
		}
		items, err := service.Releases(r.Context(), session.User.ID, page.after, page.limit+1)
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
	s.Handle("POST /api/v1/releases", true, true, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		var input struct {
			Name    *string   `json:"name"`
			Members *[]string `json:"sourceAlbumIds"`
		}
		if err := DecodeJSON(w, r, &input); err != nil {
			return err
		}
		if input.Name == nil || input.Members == nil {
			return invalidAlbumInput()
		}
		release, err := service.CreateRelease(r.Context(), session.User.ID, albums.CreateReleaseRequest{Name: *input.Name, SourceAlbumIDs: *input.Members}, r.Header.Get("Idempotency-Key"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusCreated, release)
	})
	s.Handle("GET /api/v1/releases/{id}", true, false, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		release, err := service.Release(r.Context(), session.User.ID, r.PathValue("id"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, release)
	})
	s.Handle("PUT /api/v1/releases/{id}", true, true, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		var input struct {
			ExpectedRevision *int64    `json:"expectedRevision"`
			Name             *string   `json:"name"`
			Members          *[]string `json:"sourceAlbumIds"`
		}
		if err := DecodeJSON(w, r, &input); err != nil {
			return err
		}
		if input.ExpectedRevision == nil || *input.ExpectedRevision < 1 || input.Name == nil || input.Members == nil {
			return invalidAlbumInput()
		}
		release, err := service.UpdateRelease(r.Context(), session.User.ID, r.PathValue("id"), albums.UpdateReleaseRequest{ExpectedRevision: *input.ExpectedRevision, Name: *input.Name, SourceAlbumIDs: *input.Members}, r.Header.Get("Idempotency-Key"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, release)
	})
	for _, family := range []struct{ path, kind string }{{"source-albums", "sourceAlbum"}, {"releases", "release"}} {
		base := "/api/v1/" + family.path + "/{id}"
		target := func(r *http.Request) albums.Target { return albums.Target{Kind: family.kind, ID: r.PathValue("id")} }
		s.Handle("GET "+base+"/cover-selection", true, false, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
			cover, err := service.Cover(r.Context(), session.User.ID, target(r))
			if err != nil {
				return err
			}
			return WriteJSON(w, http.StatusOK, cover)
		})
		s.Handle("PUT "+base+"/cover-selection", true, true, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
			var input struct {
				ExpectedRevision       *int64  `json:"expectedRevision"`
				ExpectedTargetRevision *int64  `json:"expectedTargetRevision"`
				ObjectID               *string `json:"objectId"`
			}
			if err := DecodeJSON(w, r, &input); err != nil {
				return err
			}
			if input.ExpectedRevision == nil || *input.ExpectedRevision < 0 || input.ExpectedTargetRevision == nil || *input.ExpectedTargetRevision < 1 || input.ObjectID == nil {
				return invalidAlbumInput()
			}
			cover, err := service.SelectCover(r.Context(), session.User.ID, albums.SelectCoverRequest{Target: target(r), ExpectedRevision: *input.ExpectedRevision, ExpectedTargetRevision: *input.ExpectedTargetRevision, ObjectID: *input.ObjectID}, r.Header.Get("Idempotency-Key"))
			if err != nil {
				return err
			}
			return WriteJSON(w, http.StatusOK, cover)
		})
		s.Handle("POST "+base+"/cover-selection/unlock", true, true, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
			var input struct {
				ExpectedRevision *int64 `json:"expectedRevision"`
			}
			if err := DecodeJSON(w, r, &input); err != nil {
				return err
			}
			if input.ExpectedRevision == nil || *input.ExpectedRevision < 0 {
				return invalidAlbumInput()
			}
			cover, err := service.UnlockCover(r.Context(), session.User.ID, albums.UnlockCoverRequest{Target: target(r), ExpectedRevision: *input.ExpectedRevision}, r.Header.Get("Idempotency-Key"))
			if err != nil {
				return err
			}
			return WriteJSON(w, http.StatusOK, cover)
		})
		s.Handle("GET "+base+"/changes", true, false, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
			page, err := albumPagination(r, false, true)
			if err != nil {
				return err
			}
			items, err := service.AlbumChanges(r.Context(), session.User.ID, target(r), page.sequence, page.limit+1)
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
	}
}

type sourceAlbumInput struct {
	Provider *string   `json:"providerInstanceId"`
	Name     *string   `json:"name"`
	Members  *[]string `json:"sourceTrackIds"`
}
type albumPage struct {
	Items     any     `json:"items"`
	NextAfter *string `json:"nextAfter"`
}

func invalidAlbumInput() error {
	return domain.Fail("INVALID_ARGUMENT", "Explicit album fields and expected revisions are required")
}
