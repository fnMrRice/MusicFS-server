package api

import (
	"net/http"
	"net/url"
	"strconv"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/plugins/onlinehost"
)

func (s *Server) RegisterOnline(service *onlinehost.Service) {
	s.registerOnlineAccounts(service)
	s.registerOnlineDisplay(service)
	s.registerOnlineEntities(service)
	s.registerOnlinePlaylists(service)
	s.Handle("GET /api/v1/online-instances/{id}/tracks/{externalId}/details", true, false, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		if _, err := onlineQuery(r); err != nil {
			return err
		}
		details, err := service.TrackDetails(r.Context(), session.User.ID, r.PathValue("id"), r.PathValue("externalId"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, details)
	})
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
		query, err := onlineQuery(r, "q", "kind", "limit", "cursor")
		if err != nil {
			return err
		}
		limit, err := onlineLimit(query)
		if err != nil {
			return err
		}
		page, err := service.Search(r.Context(), session.User.ID, r.PathValue("id"), online.SearchRequest{Kind: query.Get("kind"), Query: query.Get("q"), Limit: limit, Cursor: query.Get("cursor")})
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
		query, err := onlineQuery(r, "refresh")
		if err != nil {
			return err
		}
		refresh, err := playlistRefresh(query)
		if err != nil {
			return err
		}
		page, err := service.BrowsePlaylist(r.Context(), session.User.ID, r.PathValue("id"), r.PathValue("externalId"), refresh)
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
	s.Handle("GET /api/v1/online-instances/{id}/daily-songs", true, false, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		query, err := onlineQuery(r, "limit")
		if err != nil {
			return err
		}
		limit, err := onlineLimitOr(query, online.MaxPageItems)
		if err != nil {
			return err
		}
		page, err := service.DailySongs(r.Context(), session.User.ID, r.PathValue("id"), limit)
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
		track, err := service.Resolve(r.Context(), session.User.ID, r.PathValue("id"), r.PathValue("externalId"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, track)
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
	return onlineLimitOr(q, 20)
}

func onlineLimitOr(q url.Values, defaultLimit int) (int, error) {
	if _, present := q["limit"]; !present {
		return defaultLimit, nil
	}
	limit, err := strconv.Atoi(q.Get("limit"))
	if err != nil || limit < 1 || limit > online.MaxPageItems || strconv.Itoa(limit) != q.Get("limit") {
		return 0, domain.Fail("INVALID_ARGUMENT", "Online limit must be between 1 and 50")
	}
	return limit, nil
}
