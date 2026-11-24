package api

import (
	"io"
	"net/http"
	"strconv"

	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/plugins/onlineartwork"
)

func (s *Server) RegisterOnlineArtwork(service *onlineartwork.Service) {
	s.HandleMediaDelivery("GET /api/v1/source-tracks/{id}/online-artwork", func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		if _, err := onlineQuery(r); err != nil {
			return err
		}
		object, body, err := service.Open(r.Context(), session.User.ID, r.PathValue("id"))
		if err != nil {
			return err
		}
		defer body.Close()
		if object.MIME != "image/png" && object.MIME != "image/jpeg" {
			return domain.Fail("UNSUPPORTED", "Unsupported artwork format")
		}
		w.Header().Set("Content-Type", object.MIME)
		w.Header().Set("Content-Length", strconv.FormatInt(object.Size, 10))
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "private, no-cache")
		w.WriteHeader(http.StatusOK)
		_, err = io.CopyN(w, body, object.Size)
		return err
	})
}
