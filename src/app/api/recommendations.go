package api

import (
	"net/http"
	"net/url"
	"strconv"

	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/recommendations"
)

func (s *Server) RegisterRecommendations(service *recommendations.Service) {
	s.Handle("GET /api/v1/recommendations", true, false, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		query, err := url.ParseQuery(r.URL.RawQuery)
		if err != nil {
			return domain.Fail("INVALID_ARGUMENT", "Invalid recommendation query")
		}
		for name, values := range query {
			if name == "instanceId" {
				if len(values) > recommendations.MaxSources {
					return domain.Fail("INVALID_ARGUMENT", "Select at most eight recommendation instances")
				}
				continue
			}
			if (name != "limit" && name != "refresh" && name != "history") || len(values) != 1 {
				return domain.Fail("INVALID_ARGUMENT", "Unknown or duplicate recommendation query parameter")
			}
		}
		limit, err := onlineLimit(query)
		if err != nil {
			return err
		}
		refresh := false
		if raw, exists := query["refresh"]; exists {
			if raw[0] != "true" && raw[0] != "false" {
				return domain.Fail("INVALID_ARGUMENT", "Recommendation refresh must be true or false")
			}
			refresh = raw[0] == "true"
		}
		history := 0
		if values, exists := query["history"]; exists {
			history, err = strconv.Atoi(values[0])
			if err != nil || history < 0 || history > recommendations.MaxHistory || strconv.Itoa(history) != values[0] {
				return domain.Fail("INVALID_ARGUMENT", "Recommendation history must be between 0 and 5")
			}
		}
		if history > 0 && refresh {
			return domain.Fail("INVALID_ARGUMENT", "Historical recommendations cannot be refreshed")
		}
		result, err := service.Page(r.Context(), session.User.ID, recommendations.Request{InstanceIDs: query["instanceId"], Limit: limit, Refresh: refresh, History: history})
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, result)
	})
}
