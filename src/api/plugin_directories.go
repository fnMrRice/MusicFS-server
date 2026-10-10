package api

import (
	"net/http"

	"github.com/fnMrRice/MusicFS-server/src/auth"
	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/plugincontrol"
)

func (s *Server) registerPluginDirectories(manager *plugincontrol.Manager) {
	s.Handle("GET /api/v1/admin/plugin-instances/{id}/directories", true, false, requireAdmin(func(w http.ResponseWriter, r *http.Request, _ auth.Session) error {
		query, err := onlineQuery(r, "path", "cursor")
		if err != nil {
			return err
		}
		for key, values := range query {
			if values[0] == "" || key == "path" && len(values[0]) > 4096 || key == "cursor" && len(values[0]) > 256 {
				return domain.Fail("INVALID_ARGUMENT", "Invalid directory query")
			}
		}
		page, err := manager.Directories(r.Context(), r.PathValue("id"), query.Get("path"), query.Get("cursor"))
		if err != nil {
			return err
		}
		w.Header().Set("ETag", `"`+page.Revision+`"`)
		w.Header().Set("Cache-Control", "no-store")
		return WriteJSON(w, http.StatusOK, page)
	}))
}
