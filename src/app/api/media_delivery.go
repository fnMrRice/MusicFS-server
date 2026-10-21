package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

const MediaDeliveryTimeout = 30 * time.Second

// HandleMediaDelivery bounds delivery of an immutable small media object. It
// deliberately has a finite write deadline, unlike long-lived audio streams.
func (s *Server) HandleMediaDelivery(pattern string, handler Handler) {
	s.HandleStream(pattern, func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		ctx, cancel := context.WithTimeout(r.Context(), MediaDeliveryTimeout)
		defer cancel()
		controller := http.NewResponseController(w)
		deadline, _ := ctx.Deadline()
		if err := controller.SetWriteDeadline(deadline); err != nil && !errors.Is(err, http.ErrNotSupported) {
			return domain.Fail("TEMPORARY_FAILURE", "Media delivery deadline could not be set")
		}
		defer controller.SetWriteDeadline(time.Time{})
		return handler(w, r.WithContext(ctx), session)
	})
}
