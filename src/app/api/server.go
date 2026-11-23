// Package api maps HTTP requests to application services.
package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
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
	control controlHub
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
	s.control.connections = map[*controlConnection]string{}
	s.HandleStream("GET /api/v1/control", s.openControl)
	s.Handle("GET /api/v1/health/live", false, false, s.health)
	s.Handle("GET /api/v1/health/ready", false, false, s.health)
	s.Handle("POST /api/v1/auth/session", false, false, s.login)
	s.Handle("GET /api/v1/auth/session", true, false, s.currentSession)
	s.Handle("DELETE /api/v1/auth/session", true, true, s.logout)
	return s
}

// Handle registers a bounded control request. Register routes before serving.
func (s *Server) Handle(pattern string, authenticated, mutation bool, handler Handler) {
	s.register(pattern, authenticated, mutation, s.options.ControlTimeout, handler)
}

// HandleStream keeps request cancellation without imposing a control deadline.
func (s *Server) HandleStream(pattern string, handler Handler) {
	s.register(pattern, true, false, 0, handler)
}

func (s *Server) register(pattern string, authenticated, mutation bool, timeout time.Duration, handler Handler) {
	s.registerBounded(pattern, authenticated, mutation, timeout, MaxRequestBytes, handler)
}

func (s *Server) registerBounded(pattern string, authenticated, mutation bool, timeout time.Duration, limit int64, handler Handler) {
	authenticated = authenticated || mutation
	s.mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		if timeout > 0 {
			ctx, cancel := context.WithTimeout(r.Context(), timeout)
			defer cancel()
			r = r.WithContext(ctx)
			r.Body = http.MaxBytesReader(w, r.Body, limit)
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
	started := time.Now()
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
	_, pattern := s.mux.Handler(r)
	s.options.Logger.Log(r.Context(), slog.Level(-8), "HTTP request dispatched", "requestId", id, "route", pattern, "method", r.Method)
	defer func() {
		status := response.status
		if status == 0 {
			status = http.StatusOK
		}
		s.options.Logger.DebugContext(r.Context(), "HTTP request completed", "requestId", id, "route", pattern,
			"method", r.Method, "status", status, "bytes", response.bytes, "durationMs", time.Since(started).Milliseconds())
	}()
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
	if pattern == "" {
		WriteError(response, r, domain.Fail("NOT_FOUND", "Endpoint was not found"))
		return
	}
	s.mux.ServeHTTP(response, r)
}

func (s *Server) fail(w http.ResponseWriter, r *http.Request, err error) {
	code, status := classifyError(err)
	level := slog.LevelWarn
	if status >= 500 {
		level = slog.LevelError
	}
	attrs := []slog.Attr{slog.String("requestId", RequestID(r)), slog.String("code", code)}
	var failure *domain.Error
	if errors.As(err, &failure) && failure.Scraper != nil {
		detail := failure.Scraper
		attrs = append(attrs, slog.String("pluginId", detail.PluginID), slog.String("scraperReason", detail.Reason))
		if detail.UpstreamStatus != 0 {
			attrs = append(attrs, slog.Int("upstreamStatus", detail.UpstreamStatus))
		}
		if failure.RetryAfter != nil {
			attrs = append(attrs, slog.Int("retryAfter", *failure.RetryAfter))
		}
	}
	s.options.Logger.LogAttrs(r.Context(), level, "HTTP request failed", attrs...)
	if response, ok := w.(*responseWriter); ok && response.wroteHeader {
		panic(http.ErrAbortHandler)
	}
	WriteError(w, r, err)
}

type responseWriter struct {
	http.ResponseWriter
	wroteHeader bool
	status      int
	bytes       int64
}

func (w *responseWriter) WriteHeader(status int) {
	if !w.wroteHeader {
		w.wroteHeader = true
		w.status = status
		w.ResponseWriter.WriteHeader(status)
	}
}

func (w *responseWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += int64(n)
	return n, err
}

func (w *responseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *responseWriter) Flush() {
	_ = w.FlushError()
}

func (w *responseWriter) FlushError() error {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return http.NewResponseController(w.ResponseWriter).Flush()
}
