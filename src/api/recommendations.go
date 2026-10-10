package api

import (
	"net/http"
	"net/url"

	"github.com/fnMrRice/MusicFS-server/src/auth"
	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/recommendations"
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
			if (name != "limit" && name != "refresh") || len(values) != 1 {
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
		result, err := service.List(r.Context(), session.User.ID, recommendations.Request{InstanceIDs: query["instanceId"], Limit: limit, Refresh: refresh})
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, result)
	})
}
