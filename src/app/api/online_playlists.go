package api

import (
	"net/http"
	"net/url"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/plugins/onlinehost"
)

func (s *Server) registerOnlinePlaylists(service *onlinehost.Service) {
	// 首次汇总大目录可能包含多个受平台限流的批次，每个插件调用另有超时。
	s.register("GET /api/v1/online-instances/{id}/playlists", true, false, 5*time.Minute, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		query, err := onlineQuery(r, "limit", "cursor", "refresh")
		if err != nil {
			return err
		}
		limit, err := onlineLimit(query)
		if err != nil {
			return err
		}
		refresh, err := playlistRefresh(query)
		if err != nil {
			return err
		}
		page, err := service.BrowsePlaylists(r.Context(), session.User.ID, r.PathValue("id"), online.PlaylistsRequest{Limit: limit, Cursor: query.Get("cursor")}, refresh)
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, page)
	})
}

func playlistRefresh(q url.Values) (bool, error) {
	if !q.Has("refresh") || q.Get("refresh") == "false" {
		return false, nil
	}
	if q.Get("refresh") != "true" || q.Get("cursor") != "" {
		return false, domain.Fail("INVALID_ARGUMENT", "Refresh expects a boolean and must start from the first page")
	}
	return true, nil
}
