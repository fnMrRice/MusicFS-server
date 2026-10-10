package api

import (
	"net/http"

	"github.com/fnMrRice/MusicFS-server/src/auth"
	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/playback"
	"github.com/fnMrRice/MusicFS-server/src/stream"
)

func (s *Server) RegisterPlayback(service *playback.Service) {
	s.registerTimeline(service)
	s.Handle("POST /api/v1/playback/sessions", true, true, func(w http.ResponseWriter, r *http.Request, a auth.Session) error {
		request, err := decodeCreate(w, r)
		if err != nil {
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
			request, err := decodeControl(w, r, operation)
			if err != nil {
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

func decodeControl(w http.ResponseWriter, r *http.Request, operation string) (playback.ControlRequest, error) {
	var request playback.ControlRequest
	var position *int64
	if operation == "seek" {
		var wire struct {
			AttemptID  string `json:"attemptId"`
			PositionMs *int64 `json:"positionMs"`
		}
		if err := DecodeJSON(w, r, &wire); err != nil {
			return request, err
		}
		request.AttemptID, position = wire.AttemptID, wire.PositionMs
	} else {
		var wire struct {
			AttemptID     string `json:"attemptId"`
			PositionMs    *int64 `json:"positionMs"`
			ObservedError *struct {
				Code          *string                 `json:"code"`
				Message       *string                 `json:"message"`
				OutputProfile *playback.OutputProfile `json:"outputProfile,omitempty"`
			} `json:"observedError"`
		}
		if err := DecodeJSON(w, r, &wire); err != nil {
			return request, err
		}
		observed := wire.ObservedError
		if observed == nil || observed.Code == nil || observed.Message == nil || len(*observed.Code) > 128 || len(*observed.Message) > 1024 {
			return request, domain.Fail("INVALID_ARGUMENT", "A bounded observedError is required")
		}
		request.AttemptID, position = wire.AttemptID, wire.PositionMs
		request.ObservedError = &playback.ObservedError{Code: *observed.Code, Message: *observed.Message, OutputProfile: observed.OutputProfile}
	}
	if request.AttemptID == "" || position == nil || *position < 0 {
		return request, domain.Fail("INVALID_ARGUMENT", "attemptId and a nonnegative positionMs are required")
	}
	request.PositionMs = *position
	return request, nil
}

func decodeCreate(w http.ResponseWriter, r *http.Request) (playback.CreateRequest, error) {
	var request playback.CreateRequest
	var wire struct {
		SourceTrackID string `json:"sourceTrackId"`
		Capabilities  *struct {
			HTMLAudio *bool `json:"htmlAudio"`
			Formats   *[]struct {
				MIME    string  `json:"mime"`
				CanPlay *string `json:"canPlay"`
			} `json:"formats"`
		} `json:"capabilities"`
		Preferences *playback.Preferences `json:"preferences"`
	}
	if err := DecodeJSON(w, r, &wire); err != nil {
		return request, err
	}
	if wire.SourceTrackID == "" || wire.Capabilities == nil || wire.Capabilities.HTMLAudio == nil || !*wire.Capabilities.HTMLAudio || wire.Capabilities.Formats == nil || wire.Preferences == nil {
		return request, domain.Fail("INVALID_ARGUMENT", "sourceTrackId, capabilities, and preferences are required")
	}
	request.SourceTrackID, request.Preferences = wire.SourceTrackID, *wire.Preferences
	request.Capabilities.HTMLAudio = true
	request.Capabilities.Formats = []playback.Format{}
	if len(*wire.Capabilities.Formats) > 32 {
		return request, domain.Fail("INVALID_ARGUMENT", "Too many audio format declarations")
	}
	for _, format := range *wire.Capabilities.Formats {
		if format.MIME == "" || format.CanPlay == nil || (*format.CanPlay != "" && *format.CanPlay != "maybe" && *format.CanPlay != "probably") {
			return request, domain.Fail("INVALID_ARGUMENT", "Invalid audio format declaration")
		}
		request.Capabilities.Formats = append(request.Capabilities.Formats, playback.Format{MIME: format.MIME, CanPlay: *format.CanPlay})
	}
	return request, nil
}
