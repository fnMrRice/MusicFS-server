package api

import (
	"context"
	"net/http"
	"net/url"

	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/music"
)

// RegisterMusicLibrary 提供有独立身份的曲目资料，来源由扫描及录音关联接口管理。
func (s *Server) RegisterMusicLibrary(repo music.Repository) {
	registerMusicList(s, "tracks", repo.MusicTracks)
	registerMusicList(s, "albums", repo.MusicAlbums)
	registerMusicList(s, "artists", repo.MusicArtists)
	registerMusicList(s, "genres", repo.MusicGenres)
	registerMusicDetail(s, "tracks", repo.MusicTrack)
	registerMusicDetail(s, "albums", repo.MusicAlbum)
	registerMusicDetail(s, "artists", repo.MusicArtist)
	s.Handle("POST /api/v1/library/albums/{id}/merge-tracks", true, true, func(w http.ResponseWriter, r *http.Request, a auth.Session) error {
		var input struct {
			music.MergeTracksInput
			ExpectedMatchingRevision *int64 `json:"expectedMatchingRevision"`
		}
		if err := DecodeJSON(w, r, &input); err != nil {
			return err
		}
		if input.ExpectedMatchingRevision == nil {
			return domain.Fail("INVALID_ARGUMENT", "Expected matching revision is required")
		}
		input.MergeTracksInput.ExpectedMatchingRevision = *input.ExpectedMatchingRevision
		if err := input.MergeTracksInput.Validate(); err != nil {
			return err
		}
		track, err := repo.MergeMusicAlbumTracks(r.Context(), a.User.ID, r.PathValue("id"), input.MergeTracksInput, r.Header.Get("Idempotency-Key"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, publicTrack(track))
	})
	s.Handle("PUT /api/v1/library/artists/{id}/names", true, true, func(w http.ResponseWriter, r *http.Request, a auth.Session) error {
		var in struct {
			music.ArtistNamesInput
			ExpectedRevision int64 `json:"expectedRevision"`
		}
		if err := DecodeJSON(w, r, &in); err != nil {
			return err
		}
		item, err := repo.SaveMusicArtistNames(r.Context(), a.User.ID, r.PathValue("id"), in.ExpectedRevision, in.ArtistNamesInput, r.Header.Get("Idempotency-Key"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, item)
	})
	s.Handle("GET /api/v1/library/sources/{id}/track", true, false, func(w http.ResponseWriter, r *http.Request, a auth.Session) error {
		if r.URL.RawQuery != "" {
			return invalidCatalogParameter()
		}
		track, err := repo.MusicTrackForSource(r.Context(), a.User.ID, r.PathValue("id"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, publicTrack(track))
	})
	s.Handle("POST /api/v1/library/tracks", true, true, func(w http.ResponseWriter, r *http.Request, a auth.Session) error {
		var in music.TrackInput
		if err := DecodeJSON(w, r, &in); err != nil {
			return err
		}
		t, err := repo.SaveMusicTrack(r.Context(), a.User.ID, "", 0, in, r.Header.Get("Idempotency-Key"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusCreated, publicTrack(t))
	})
	s.Handle("PUT /api/v1/library/tracks/{id}", true, true, func(w http.ResponseWriter, r *http.Request, a auth.Session) error {
		var in struct {
			music.TrackInput
			ExpectedRevision int64 `json:"expectedRevision"`
		}
		if err := DecodeJSON(w, r, &in); err != nil {
			return err
		}
		t, err := repo.SaveMusicTrack(r.Context(), a.User.ID, r.PathValue("id"), in.ExpectedRevision, in.TrackInput, r.Header.Get("Idempotency-Key"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, publicTrack(t))
	})
	s.Handle("POST /api/v1/library/albums", true, true, func(w http.ResponseWriter, r *http.Request, a auth.Session) error {
		var in music.AlbumFields
		if err := DecodeJSON(w, r, &in); err != nil {
			return err
		}
		item, err := repo.SaveMusicAlbum(r.Context(), a.User.ID, "", 0, in, r.Header.Get("Idempotency-Key"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusCreated, publicAlbum(item))
	})
	s.Handle("PUT /api/v1/library/albums/{id}", true, true, func(w http.ResponseWriter, r *http.Request, a auth.Session) error {
		var in struct {
			music.AlbumFields
			ExpectedRevision int64 `json:"expectedRevision"`
		}
		if err := DecodeJSON(w, r, &in); err != nil {
			return err
		}
		item, err := repo.SaveMusicAlbum(r.Context(), a.User.ID, r.PathValue("id"), in.ExpectedRevision, in.AlbumFields, r.Header.Get("Idempotency-Key"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, publicAlbum(item))
	})
}

func registerMusicList[T any](s *Server, kind string, load func(context.Context, string, music.Query) (T, error)) {
	s.Handle("GET /api/v1/library/"+kind, true, false, func(w http.ResponseWriter, r *http.Request, a auth.Session) error {
		params, err := url.ParseQuery(r.URL.RawQuery)
		if err != nil {
			return invalidCatalogParameter()
		}
		allowed := map[string]bool{"limit": false, "after": false, "q": false, "providerInstanceId": false}
		if kind == "tracks" {
			for _, field := range []string{"artistId", "albumId", "genre", "unknown", "sourceOnly"} {
				allowed[field] = false
			}
		}
		if kind == "albums" {
			allowed["unscrapedOnly"] = false
		}
		if err = catalogParameters(params, allowed); err != nil {
			return err
		}
		for _, key := range []string{"sourceOnly", "unscrapedOnly"} {
			if params.Has(key) && params.Get(key) != "true" && params.Get(key) != "false" {
				return invalidCatalogParameter()
			}
		}
		limit, err := catalogLimit(params)
		if err != nil {
			return err
		}
		query := music.Query{Limit: limit, After: params.Get("after"), Q: params.Get("q"), ProviderID: params.Get("providerInstanceId"), ArtistID: params.Get("artistId"), AlbumID: params.Get("albumId"), Genre: params.Get("genre"), Unknown: params.Get("unknown"), SourceOnly: params.Get("sourceOnly") == "true"}
		query.UnscrapedOnly = params.Get("unscrapedOnly") == "true"
		res, err := load(r.Context(), a.User.ID, query)
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, publicMusicValue(res))
	})
}

func registerMusicDetail[T any](s *Server, kind string, load func(context.Context, string, string) (T, error)) {
	s.Handle("GET /api/v1/library/"+kind+"/{id}", true, false, func(w http.ResponseWriter, r *http.Request, a auth.Session) error {
		if r.URL.RawQuery != "" {
			return domain.Fail("INVALID_ARGUMENT", "Unexpected music detail query")
		}
		res, err := load(r.Context(), a.User.ID, r.PathValue("id"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, publicMusicValue(res))
	})
}
