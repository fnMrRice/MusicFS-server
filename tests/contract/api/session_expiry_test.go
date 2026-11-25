package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/synctest"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/app/api"
	"github.com/fnMrRice/MusicFS-server/src/core/auth"
)

func TestHTTPSessionIdleTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		repo := &memoryAuth{sessions: map[string]auth.Session{}}
		server := api.New(api.Options{Auth: auth.New(repo, testToken, []string{origin}, time.Minute), SecureCookie: true})
		server.Handle("POST /api/v1/action", true, true, func(w http.ResponseWriter, _ *http.Request, _ auth.Session) error {
			w.WriteHeader(http.StatusNoContent)
			return nil
		})
		cookie, original := login(t, server)
		request := func(method, path, csrf string) *httptest.ResponseRecorder {
			r := httptest.NewRequest(method, path, nil)
			r.AddCookie(cookie)
			r.Header.Set("Origin", origin)
			r.Header.Set("X-CSRF-Token", csrf)
			w := httptest.NewRecorder()
			server.ServeHTTP(w, r)
			return w
		}
		time.Sleep(45 * time.Second)
		denied := request("POST", "/api/v1/action", "invalid")
		control := request("GET", "/api/v1/control", "")
		authID := auth.Digest(cookie.Value)
		stored, err := repo.FindAuth(t.Context(), authID)
		if denied.Code != 403 || len(denied.Result().Cookies()) != 0 || len(control.Result().Cookies()) != 0 ||
			err != nil || !stored.ExpiresAt.Equal(original.ExpiresAt) {
			t.Fatal("rejected mutation or control handshake renewed the session")
		}
		w := request("POST", "/api/v1/action", original.CSRFToken)
		cookies := w.Result().Cookies()
		if w.Code != 204 || len(cookies) != 1 || cookies[0].Value != cookie.Value || !cookies[0].HttpOnly ||
			!cookies[0].Secure || cookies[0].SameSite != http.SameSiteStrictMode || cookies[0].MaxAge != 60 ||
			!cookies[0].Expires.Equal(time.Now().Add(time.Minute)) {
			t.Fatal("authenticated request did not refresh the session cookie")
		}
		time.Sleep(30 * time.Second)
		w = request("GET", "/api/v1/auth/session", "")
		var renewed auth.Session
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &renewed) != nil || renewed.CSRFToken != original.CSRFToken ||
			!renewed.ExpiresAt.Equal(time.Now().Add(time.Minute)) {
			t.Fatal("session did not survive its original deadline")
		}
		time.Sleep(time.Minute)
		w = request("GET", "/api/v1/auth/session", "")
		if w.Code != 401 || len(w.Result().Cookies()) != 0 {
			t.Fatal("idle session was renewed after expiry")
		}
	})
}

func TestControlCommandLeavesHTTPIdleDeadlineUnchanged(t *testing.T) {
	repo := &memoryAuth{sessions: map[string]auth.Session{}}
	server := api.New(api.Options{Auth: auth.New(repo, testToken, []string{origin}, time.Hour), SecureCookie: true})
	server.Handle("GET /api/v1/lifecycle", true, false, func(w http.ResponseWriter, _ *http.Request, _ auth.Session) error {
		return api.WriteJSON(w, http.StatusOK, struct{}{})
	})
	cookie, session := login(t, server)
	session.ID = auth.Digest(cookie.Value)
	session.ExpiresAt = time.Now().Add(time.Minute)
	if err := repo.SaveAuth(t.Context(), session); err != nil {
		t.Fatal(err)
	}
	host := controlHTTP(t, server, nil)
	conn := dialControl(t, host, cookie, session.CSRFToken)
	readControl(t, conn, "ready")
	if reply := controlCall(t, conn, "read-status", "GET", "/lifecycle", nil, ""); reply.Status != http.StatusOK {
		t.Fatal("control request failed", reply)
	}
	stored, err := repo.FindAuth(t.Context(), session.ID)
	if err != nil || !stored.ExpiresAt.Equal(session.ExpiresAt) {
		t.Fatal("control request renewed the HTTP cookie's server record", err)
	}
}
