package api

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"

	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/library/personal"
)

func (s *Server) RegisterPersonal(service *personal.Service) {
	s.Handle("GET /api/v1/me/profile", true, false, func(w http.ResponseWriter, r *http.Request, a auth.Session) error {
		p, err := service.PersonalProfile(r.Context(), a.User.ID)
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, p)
	})
	s.Handle("PUT /api/v1/me/profile", true, true, func(w http.ResponseWriter, r *http.Request, a auth.Session) error {
		var input struct {
			Nickname json.RawMessage `json:"nickname"`
		}
		if err := DecodeJSON(w, r, &input); err != nil {
			return err
		}
		var nickname *string
		if len(input.Nickname) == 0 || json.Unmarshal(input.Nickname, &nickname) != nil {
			return personal.Invalid()
		}
		p, err := service.SavePersonalProfile(r.Context(), a.User.ID, nickname)
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, p)
	})
	for _, kind := range []string{personal.Favorites, personal.Recent} {
		s.Handle("GET /api/v1/me/"+kind, true, false, func(w http.ResponseWriter, r *http.Request, a auth.Session) error {
			limit, cursor, err := personalPagination(r)
			if err != nil {
				return err
			}
			p, err := service.PersonalCollection(r.Context(), a.User.ID, kind, cursor, limit)
			if err != nil {
				return err
			}
			return WriteJSON(w, http.StatusOK, p)
		})
		for _, suffix := range []string{"", "/{id}"} {
			s.Handle("DELETE /api/v1/me/"+kind+suffix, true, true, func(w http.ResponseWriter, r *http.Request, a auth.Session) error {
				if err := service.DeletePersonalCollection(r.Context(), a.User.ID, kind, r.PathValue("id")); err != nil {
					return err
				}
				w.WriteHeader(http.StatusNoContent)
				return nil
			})
		}
	}
	s.Handle("PUT /api/v1/me/favorites/{id}", true, true, func(w http.ResponseWriter, r *http.Request, a auth.Session) error {
		if r.ContentLength != 0 {
			return personal.Invalid()
		}
		entry, err := service.PutFavorite(r.Context(), a.User.ID, r.PathValue("id"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, entry)
	})
	s.Handle("POST /api/v1/me/recently-played", true, true, func(w http.ResponseWriter, r *http.Request, a auth.Session) error {
		var input personal.PlaybackStarted
		if err := DecodeJSON(w, r, &input); err != nil {
			return err
		}
		entry, err := service.RecordPlaybackStarted(r.Context(), a, input)
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, entry)
	})
}

func personalPagination(r *http.Request) (int, string, error) {
	limit, cursor := 50, ""
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return 0, "", personal.Invalid()
	}
	for key, values := range query {
		if len(values) != 1 || values[0] == "" {
			return 0, "", personal.Invalid()
		}
		switch key {
		case "limit":
			value, err := strconv.Atoi(values[0])
			if err != nil || value < 1 || value > 100 || strconv.Itoa(value) != values[0] {
				return 0, "", personal.Invalid()
			}
			limit = value
		case "cursor":
			cursor = values[0]
		default:
			return 0, "", personal.Invalid()
		}
	}
	return limit, cursor, nil
}
