package api

import (
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/fnMrRice/MusicFS-server/src/auth"
	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/mediastore"
)

func derivedMedia(service mediaAPI) *mediastore.DerivedService {
	if provider, ok := service.(interface {
		Derived() *mediastore.DerivedService
	}); ok {
		return provider.Derived()
	}
	return nil
}
func mediaNotFound(err error) bool {
	var e *domain.Error
	return errors.As(err, &e) && e.Code == "NOT_FOUND"
}
func (s *Server) registerDerivedMedia(service *mediastore.DerivedService) {
	if service == nil {
		return
	}
	s.Handle("POST /api/v1/media/{id}/thumbnail", true, true, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		var input struct{}
		if err := DecodeJSON(w, r, &input); err != nil {
			return err
		}
		o, err := service.Thumbnail(r.Context(), session.User.ID, r.PathValue("id"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, o)
	})
	s.Handle("GET /api/v1/metadata-jobs/{id}/documents", true, false, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		items, err := service.ListScrapeDocuments(r.Context(), session.User.ID, r.PathValue("id"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, struct {
			Items []mediastore.DerivedObject `json:"items"`
		}{items})
	})
}
func deliverDerived(w http.ResponseWriter, r *http.Request, session auth.Session, service *mediastore.DerivedService) error {
	o, body, err := service.Open(r.Context(), session.User.ID, r.PathValue("id"))
	if err != nil {
		return err
	}
	defer body.Close()
	disposition := `inline; filename="thumbnail.png"`
	if o.Kind == "scrape-document" {
		disposition = `attachment; filename="scrape-document.json"`
	}
	w.Header().Set("Content-Type", o.MIME)
	w.Header().Set("Content-Length", strconv.FormatInt(o.Size, 10))
	w.Header().Set("Content-Disposition", disposition)
	w.Header().Set("Accept-Ranges", "none")
	w.WriteHeader(http.StatusOK)
	_, err = io.CopyN(w, body, o.Size)
	return err
}
