package api

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/metadata/enrichment"
)

type metadataAutomationAPI interface {
	ScanResults(context.Context, string, string, string, int) (enrichment.AutomationPage, error)
}

func (s *Server) RegisterMetadataAutomation(service metadataAutomationAPI) {
	s.Handle("GET /api/v1/scan-jobs/{id}/metadata-results", true, false, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		query, err := url.ParseQuery(r.URL.RawQuery)
		if err != nil {
			return domain.Fail("INVALID_ARGUMENT", "Invalid metadata result query")
		}
		for name, values := range query {
			if (name != "cursor" && name != "limit") || len(values) != 1 {
				return domain.Fail("INVALID_ARGUMENT", "Invalid metadata result query")
			}
		}
		limit := 50
		if value, exists := query["limit"]; exists {
			var err error
			limit, err = strconv.Atoi(value[0])
			if err != nil || limit < 1 || limit > 200 {
				return domain.Fail("INVALID_ARGUMENT", "Metadata result limit must be between 1 and 200")
			}
		}
		cursor := query.Get("cursor")
		if len(cursor) > 256 {
			return domain.Fail("INVALID_ARGUMENT", "Invalid metadata result cursor")
		}
		page, err := service.ScanResults(r.Context(), session.User.ID, r.PathValue("id"), cursor, limit)
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, page)
	})
}
