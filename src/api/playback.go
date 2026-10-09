package api

import (
	"github.com/fnMrRice/MusicFS-server/src/auth"
	"github.com/fnMrRice/MusicFS-server/src/playback"
	"github.com/fnMrRice/MusicFS-server/src/stream"
	"net/http"
)

func (s *Server) RegisterPlayback(service *playback.Service) {
	s.Handle("POST /api/v1/playback/sessions", true, true, func(w http.ResponseWriter, r *http.Request, a auth.Session) error {
		var request playback.CreateRequest
		if err := DecodeJSON(w, r, &request); err != nil {
			return err
		}
		d, err := service.Create(r.Context(), a, request, r.Header.Get("Idempotency-Key"))
		if err != nil {
			return err
		}
		return WriteJSON(w, http.StatusCreated, d)
	})
	s.Handle("DELETE /api/v1/playback/sessions/{id}", true, true, func(w http.ResponseWriter, r *http.Request, a auth.Session) error {
		if err := service.Stop(r.Context(), a, r.PathValue("id")); err != nil {
			return err
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	})
	for _, operation := range []string{"seek", "recover"} {
		s.Handle("POST /api/v1/playback/sessions/{id}/"+operation, true, true, func(w http.ResponseWriter, r *http.Request, a auth.Session) error {
			var request playback.ControlRequest
			if err := DecodeJSON(w, r, &request); err != nil {
				return err
			}
			d, err := service.Control(r.Context(), a, r.PathValue("id"), operation, request, r.Header.Get("Idempotency-Key"))
			if err != nil {
				return err
			}
			return WriteJSON(w, http.StatusOK, d)
		})
	}
	s.HandleStream("GET /api/v1/playback/sessions/{id}/attempts/{attempt}/stream", func(w http.ResponseWriter, r *http.Request, a auth.Session) error {
		media, err := service.OpenStream(r.Context(), a, r.PathValue("id"), r.PathValue("attempt"), r.Method == http.MethodHead)
		if err != nil {
			return err
		}
		return stream.Serve(w, r, media)
	})
}
