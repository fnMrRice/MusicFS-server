package api

import (
	"net/http"
	"net/url"
	"strconv"

	"github.com/fnMrRice/MusicFS-server/src/auth"
	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/online"
	"github.com/fnMrRice/MusicFS-server/src/onlinehost"
)

func (s *Server) RegisterOnline(service *onlinehost.Service) {
	s.Handle("GET /api/v1/online-instances", true, false, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		if _, err := onlineQuery(r); err != nil {
			return err
		}
		items, err := service.Instances(r.Context(), session.User.ID)
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, struct {
			Items []onlinehost.Instance `json:"items"`
		}{items})
	})
	s.Handle("GET /api/v1/online-instances/{id}/search", true, false, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		query, err := onlineQuery(r, "q", "limit", "cursor")
		if err != nil {
			return err
		}
		limit, err := onlineLimit(query)
		if err != nil {
			return err
		}
		page, err := service.Search(r.Context(), session.User.ID, r.PathValue("id"), online.SearchRequest{Query: query.Get("q"), Limit: limit, Cursor: query.Get("cursor")})
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, page)
	})
	s.Handle("GET /api/v1/online-instances/{id}/tracks/{externalId}", true, false, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		if _, err := onlineQuery(r); err != nil {
			return err
		}
		track, err := service.Metadata(r.Context(), session.User.ID, r.PathValue("id"), r.PathValue("externalId"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, track)
	})
	s.Handle("GET /api/v1/online-instances/{id}/playlists/{externalId}", true, false, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		if _, err := onlineQuery(r); err != nil {
			return err
		}
		page, err := service.ObservePlaylist(r.Context(), session.User.ID, r.PathValue("id"), r.PathValue("externalId"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, page)
	})
	s.Handle("GET /api/v1/online-instances/{id}/recommendations", true, false, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		query, err := onlineQuery(r, "limit")
		if err != nil {
			return err
		}
		limit, err := onlineLimit(query)
		if err != nil {
			return err
		}
		page, err := service.PublicRecommendations(r.Context(), session.User.ID, r.PathValue("id"), limit)
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, page)
	})
	s.Handle("POST /api/v1/online-instances/{id}/tracks/{externalId}/resolve", true, true, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		if _, err := onlineQuery(r); err != nil {
			return err
		}
		var input struct{}
		if err := DecodeJSON(w, r, &input); err != nil {
			return err
		}
		return service.Resolve(r.Context(), session.User.ID, r.PathValue("id"), r.PathValue("externalId"))
	})
}

func onlineQuery(r *http.Request, allowed ...string) (url.Values, error) {
	q, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return nil, domain.Fail("INVALID_ARGUMENT", "Invalid online query")
	}
	for key, values := range q {
		valid := false
		for _, name := range allowed {
			valid = valid || key == name
		}
		if !valid || len(values) != 1 {
			return nil, domain.Fail("INVALID_ARGUMENT", "Unknown or duplicate online query parameter")
		}
	}
	return q, nil
}

func onlineLimit(q url.Values) (int, error) {
	if _, present := q["limit"]; !present {
		return 20, nil
	}
	limit, err := strconv.Atoi(q.Get("limit"))
	if err != nil || limit < 1 || limit > online.MaxPageItems || strconv.Itoa(limit) != q.Get("limit") {
		return 0, domain.Fail("INVALID_ARGUMENT", "Online limit must be between 1 and 50")
	}
	return limit, nil
}
