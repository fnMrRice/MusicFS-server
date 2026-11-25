package api

import (
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/core/version"
)

func (s *Server) health(w http.ResponseWriter, r *http.Request, _ auth.Session) error {
	ready := s.options.Ready == nil || s.options.Ready()
	status := http.StatusOK
	if strings.HasSuffix(r.URL.Path, "/ready") && !ready {
		status = http.StatusServiceUnavailable
	}
	return WriteJSON(w, status, struct {
		Product       string `json:"product"`
		InstanceID    string `json:"instanceId"`
		ServerVersion string `json:"serverVersion"`
		APIVersion    string `json:"apiVersion"`
		Ready         bool   `json:"ready"`
	}{"MusicFS", s.options.InstanceID, version.Server, "1", ready})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request, _ auth.Session) error {
	if s.options.Auth == nil {
		return domain.Fail("TEMPORARY_FAILURE", "Authentication service is unavailable")
	}
	header := r.Header.Get("Authorization")
	scheme, credential, found := strings.Cut(header, " ")
	if !found || !strings.EqualFold(scheme, "Bearer") || credential == "" || len(credential) > 4096 || strings.ContainsAny(credential, " \r\n\t") {
		return domain.Fail("AUTH_REQUIRED", "A Bearer credential is required")
	}
	remote, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		remote = r.RemoteAddr
	}
	session, cookieValue, err := s.options.Auth.Login(r.Context(), credential, r.Header.Get("Origin"), remote)
	if err != nil {
		return err
	}
	s.setSessionCookie(w, cookieValue, session.ExpiresAt)
	s.options.Logger.InfoContext(r.Context(), "User signed in", "requestId", RequestID(r), "userId", session.User.ID)
	return WriteJSON(w, http.StatusOK, session)
}

func (s *Server) currentSession(w http.ResponseWriter, _ *http.Request, session auth.Session) error {
	return WriteJSON(w, http.StatusOK, session)
}

func (s *Server) requestSession(w http.ResponseWriter, r *http.Request, mutation bool) (auth.Session, error) {
	if s.options.Auth == nil {
		return auth.Session{}, domain.Fail("TEMPORARY_FAILURE", "Authentication service is unavailable")
	}
	cookie, err := r.Cookie(CookieName)
	if err != nil {
		return auth.Session{}, domain.Fail("AUTH_REQUIRED", "Login is required")
	}
	session, err := s.options.Auth.Authenticate(r.Context(), cookie.Value)
	if err != nil {
		return auth.Session{}, err
	}
	if mutation || r.Method != http.MethodGet && r.Method != http.MethodHead {
		if err := s.options.Auth.CheckMutation(session, r.Header.Get("Origin"), r.Header.Get("X-CSRF-Token")); err != nil {
			return auth.Session{}, err
		}
	}
	if mutation && s.options.Ready != nil && !s.options.Ready() {
		return auth.Session{}, domain.Fail("TEMPORARY_FAILURE", "Server is draining")
	}
	// 登出由处理函数删除 Cookie，控制连接仅验证会话，不作为用户活动续期。
	if r.Pattern == "DELETE /api/v1/auth/session" || r.Pattern == "GET /api/v1/control" || r.Context().Value(controlDispatchKey{}) != nil {
		return session, nil
	}
	session, err = s.options.Auth.Renew(r.Context(), session)
	if err != nil {
		return auth.Session{}, err
	}
	s.setSessionCookie(w, cookie.Value, session.ExpiresAt)
	return session, nil
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request, session auth.Session) error {
	if err := s.options.Auth.Revoke(r.Context(), session.ID); err != nil {
		return err
	}
	http.SetCookie(w, s.cookie("", time.Unix(1, 0).UTC(), -1))
	s.options.Logger.InfoContext(r.Context(), "User signed out", "requestId", RequestID(r), "userId", session.User.ID)
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) cookie(value string, expiry time.Time, maxAge int) *http.Cookie {
	return &http.Cookie{Name: CookieName, Value: value, Path: "/api/v1", Expires: expiry, MaxAge: maxAge,
		HttpOnly: true, Secure: s.options.SecureCookie, SameSite: http.SameSiteStrictMode}
}

func (s *Server) setSessionCookie(w http.ResponseWriter, value string, expiry time.Time) {
	maxAge := int((time.Until(expiry) + time.Second - 1) / time.Second)
	http.SetCookie(w, s.cookie(value, expiry, max(1, maxAge)))
}
