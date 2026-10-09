// Package api maps HTTP requests to application services.
package api

import (
	"context"
	"log/slog"
	"net/http"
	"regexp"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/auth"
	"github.com/fnMrRice/MusicFS-server/src/domain"
)

const CookieName = "musicfs_session"
const MaxRequestBytes = 64 << 10

type Options struct {
	Auth           *auth.Service
	InstanceID     string
	Ready          func() bool
	SecureCookie   bool
	Logger         *slog.Logger
	ControlTimeout time.Duration
}

type Handler func(http.ResponseWriter, *http.Request, auth.Session) error

type Server struct {
	mux     *http.ServeMux
	options Options
}

type requestIDKey struct{}

var safeRequestID = regexp.MustCompile(`^[A-Za-z0-9_-]{8,128}$`)

func New(options Options) *Server {
	if options.Logger == nil {
		options.Logger = slog.Default()
	}
	if options.ControlTimeout <= 0 {
		options.ControlTimeout = 15 * time.Second
	}
	s := &Server{mux: http.NewServeMux(), options: options}
	s.Handle("GET /api/v1/health/live", false, false, s.health)
	s.Handle("GET /api/v1/health/ready", false, false, s.health)
	s.Handle("POST /api/v1/auth/session", false, false, s.login)
	s.Handle("GET /api/v1/auth/session", true, false, s.currentSession)
	s.Handle("DELETE /api/v1/auth/session", true, true, s.logout)
	return s
}

// Handle registers a bounded control request. Register routes before serving.
func (s *Server) Handle(pattern string, authenticated, mutation bool, handler Handler) {
	s.register(pattern, authenticated, mutation, false, handler)
}

// HandleStream keeps request cancellation without imposing a control deadline.
func (s *Server) HandleStream(pattern string, handler Handler) {
	s.register(pattern, true, false, true, handler)
}

func (s *Server) register(pattern string, authenticated, mutation, stream bool, handler Handler) {
	authenticated = authenticated || mutation
	s.mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		if !stream {
			ctx, cancel := context.WithTimeout(r.Context(), s.options.ControlTimeout)
			defer cancel()
			r = r.WithContext(ctx)
			r.Body = http.MaxBytesReader(w, r.Body, MaxRequestBytes)
		}
		var session auth.Session
		var err error
		if authenticated {
			if s.options.Auth == nil {
				err = domain.Fail("TEMPORARY_FAILURE", "Authentication service is unavailable")
			} else if cookie, cookieErr := r.Cookie(CookieName); cookieErr != nil {
				err = domain.Fail("AUTH_REQUIRED", "Login is required")
			} else {
				session, err = s.options.Auth.Authenticate(r.Context(), cookie.Value)
			}
		}
		if err == nil && mutation {
			err = s.options.Auth.CheckMutation(session, r.Header.Get("Origin"), r.Header.Get("X-CSRF-Token"))
		}
		if err == nil && mutation && s.options.Ready != nil && !s.options.Ready() {
			err = domain.Fail("TEMPORARY_FAILURE", "Server is draining")
		}
		if err == nil {
			err = handler(w, r, session)
		}
		if err != nil {
			s.fail(w, r, err)
		}
	})
}

func RequestID(r *http.Request) string {
	id, _ := r.Context().Value(requestIDKey{}).(string)
	return id
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id := r.Header.Get("X-Request-ID")
	if !safeRequestID.MatchString(id) {
		id = domain.NewID()
	}
	r = r.WithContext(context.WithValue(r.Context(), requestIDKey{}, id))
	w.Header().Set("X-Request-ID", id)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	response := &responseWriter{ResponseWriter: w}
	defer func() {
		if recovered := recover(); recovered != nil {
			if recovered == http.ErrAbortHandler {
				panic(recovered)
			}
			s.options.Logger.ErrorContext(r.Context(), "HTTP handler panicked", "requestId", id)
			if response.wroteHeader {
				panic(http.ErrAbortHandler)
			}
			WriteError(response, r, domain.Fail("TEMPORARY_FAILURE", "Request could not be completed"))
		}
	}()
	_, pattern := s.mux.Handler(r)
	if pattern == "" {
		WriteError(response, r, domain.Fail("NOT_FOUND", "Endpoint was not found"))
		return
	}
	s.mux.ServeHTTP(response, r)
}

func (s *Server) fail(w http.ResponseWriter, r *http.Request, err error) {
	code, _ := classifyError(err)
	s.options.Logger.WarnContext(r.Context(), "HTTP request failed", "requestId", RequestID(r), "code", code)
	if response, ok := w.(*responseWriter); ok && response.wroteHeader {
		panic(http.ErrAbortHandler)
	}
	WriteError(w, r, err)
}

type responseWriter struct {
	http.ResponseWriter
	wroteHeader bool
}

func (w *responseWriter) WriteHeader(status int) {
	if !w.wroteHeader {
		w.wroteHeader = true
		w.ResponseWriter.WriteHeader(status)
	}
}

func (w *responseWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}

func (w *responseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *responseWriter) Flush() {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}
