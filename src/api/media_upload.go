package api

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/auth"
	"github.com/fnMrRice/MusicFS-server/src/domain"
)

const MaxMediaUploadBytes int64 = 8 << 20
const MediaUploadTimeout = 30 * time.Second

// HandleMediaUpload is the only larger request-body registration. Authentication,
// Origin/CSRF and draining checks still run before the body can reach the service.
func (s *Server) HandleMediaUpload(pattern string, handler Handler) {
	s.register(pattern, true, true, true, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		if r.ContentLength > MaxMediaUploadBytes {
			return domain.Fail("INVALID_ARGUMENT", "Media upload exceeds the HTTP size limit")
		}
		ctx, cancel := context.WithTimeout(r.Context(), MediaUploadTimeout)
		defer cancel()
		controller := http.NewResponseController(w)
		deadline, _ := ctx.Deadline()
		if err := controller.SetReadDeadline(deadline); err != nil && !errors.Is(err, http.ErrNotSupported) {
			return domain.Fail("TEMPORARY_FAILURE", "Media upload deadline could not be set")
		}
		defer controller.SetReadDeadline(time.Time{})
		r = r.WithContext(ctx)
		r.Body = http.MaxBytesReader(w, r.Body, MaxMediaUploadBytes)
		err := handler(w, r, session)
		if err != nil {
			var oversized *http.MaxBytesError
			var timeout net.Error
			if errors.As(err, &oversized) {
				return domain.Fail("INVALID_ARGUMENT", "Media upload exceeds the HTTP size limit")
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if errors.As(err, &timeout) && timeout.Timeout() {
				return domain.Fail("TEMPORARY_FAILURE", "Media upload timed out")
			}
		}
		return err
	})
}
