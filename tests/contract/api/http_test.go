package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/app/api"
	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

const origin = "http://127.0.0.1:8080"
const testToken = "test-credential-not-for-production"

type memoryAuth struct {
	mu       sync.Mutex
	sessions map[string]auth.Session
}

func (m *memoryAuth) SaveAuth(_ context.Context, session auth.Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[session.ID] = session
	return nil
}
func (m *memoryAuth) FindAuth(_ context.Context, id string) (auth.Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	session, ok := m.sessions[id]
	if !ok {
		return auth.Session{}, errors.New("session not found")
	}
	return session, nil
}
func (m *memoryAuth) DeleteAuth(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, id)
	return nil
}

func testServer() (*api.Server, *bytes.Buffer) {
	logs := &bytes.Buffer{}
	a := auth.New(&memoryAuth{sessions: map[string]auth.Session{}}, testToken, []string{origin}, time.Hour)
	return api.New(api.Options{Auth: a, InstanceID: "test-instance", SecureCookie: true,
		Logger: slog.New(slog.NewJSONHandler(logs, nil))}), logs
}

func login(t *testing.T, server *api.Server) (*http.Cookie, auth.Session) {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/auth/session", nil)
	r.Header.Set("Authorization", "Bearer "+testToken)
	r.Header.Set("Origin", origin)
	w := httptest.NewRecorder()
	server.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("login returned %d: %s", w.Code, w.Body.String())
	}
	testkit.ValidateHTTP(t, "Session", w.Body.Bytes())
	var session auth.Session
	if err := json.Unmarshal(w.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != api.CookieName || !cookies[0].HttpOnly || !cookies[0].Secure || cookies[0].SameSite != http.SameSiteStrictMode || cookies[0].Path != "/api/v1" {
		t.Fatal("invalid session cookie attributes")
	}
	return cookies[0], session
}

func TestAuthHTTPFlowAndCSRF(t *testing.T) {
	server, _ := testServer()
	unauthorized := httptest.NewRecorder()
	server.ServeHTTP(unauthorized, httptest.NewRequest("GET", "/api/v1/auth/session", nil))
	if unauthorized.Code != 401 {
		t.Fatal("anonymous session read was accepted")
	}
	testkit.ValidateHTTP(t, "Error", unauthorized.Body.Bytes())
	cookie, session := login(t, server)
	for _, test := range []struct {
		origin string
		csrf   string
	}{{origin, ""}, {"https://untrusted.invalid", session.CSRFToken}} {
		r := httptest.NewRequest("DELETE", "/api/v1/auth/session", nil)
		r.AddCookie(cookie)
		r.Header.Set("Origin", test.origin)
		r.Header.Set("X-CSRF-Token", test.csrf)
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatal("invalid mutation was accepted")
		}
	}
	read := httptest.NewRequest("GET", "/api/v1/auth/session", nil)
	read.AddCookie(cookie)
	w := httptest.NewRecorder()
	server.ServeHTTP(w, read)
	testkit.ValidateHTTP(t, "Session", w.Body.Bytes())
	r := httptest.NewRequest("DELETE", "/api/v1/auth/session", nil)
	r.AddCookie(cookie)
	r.Header.Set("Origin", origin)
	r.Header.Set("X-CSRF-Token", session.CSRFToken)
	w = httptest.NewRecorder()
	server.ServeHTTP(w, r)
	if w.Code != 204 || w.Result().Cookies()[0].MaxAge != -1 {
		t.Fatal("logout did not revoke cookie")
	}
	w = httptest.NewRecorder()
	server.ServeHTTP(w, read)
	if w.Code != 401 {
		t.Fatal("revoked cookie was accepted")
	}
}

func TestLoginOriginAndRateLimit(t *testing.T) {
	server, _ := testServer()
	for i := 0; i < 7; i++ {
		r := httptest.NewRequest("POST", "/api/v1/auth/session", nil)
		r.Header.Set("Authorization", "Bearer invalid-credential")
		r.Header.Set("Origin", origin)
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		if i < 5 && w.Code != 401 || i >= 5 && w.Code != 429 {
			t.Fatalf("attempt %d returned %d", i, w.Code)
		}
	}
	r := httptest.NewRequest("POST", "/api/v1/auth/session", nil)
	r.Header.Set("Authorization", "Bearer "+testToken)
	r.Header.Set("Origin", "https://untrusted.invalid")
	w := httptest.NewRecorder()
	server.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("cross-origin login was accepted")
	}
}

func TestHTTPDecodeAndRedaction(t *testing.T) {
	server, logs := testServer()
	server.Handle("POST /api/v1/test", false, false, func(w http.ResponseWriter, r *http.Request, _ auth.Session) error {
		var request struct {
			Name string `json:"name"`
		}
		return api.DecodeJSON(w, r, &request)
	})
	for _, body := range []string{"{", "null", `{"private":"secret"}`, `{} {}`, `{"name":"` + strings.Repeat("x", api.MaxRequestBytes) + `"}`} {
		r := httptest.NewRequest("POST", "/api/v1/test", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		if w.Code != 400 {
			t.Fatalf("invalid JSON returned %d", w.Code)
		}
		testkit.ValidateHTTP(t, "Error", w.Body.Bytes())
	}
	server.Handle("GET /api/v1/failure", false, false, func(http.ResponseWriter, *http.Request, auth.Session) error {
		return errors.New("private C:\\secret\\song.wav token=private-token")
	})
	r := httptest.NewRequest("GET", "/api/v1/failure?token=private-query", nil)
	r.Header.Set("X-Request-ID", "safe-request-1")
	w := httptest.NewRecorder()
	server.ServeHTTP(w, r)
	if w.Code != 500 || w.Header().Get("X-Request-ID") != "safe-request-1" {
		t.Fatal("incorrect error mapping or request ID")
	}
	if strings.Contains(w.Body.String()+logs.String(), "private") || strings.Contains(logs.String(), testToken) {
		t.Fatal("sensitive error detail appeared in response or logs")
	}
	testkit.ValidateHTTP(t, "Error", w.Body.Bytes())
}

func TestHealthPanicAndStreamFailure(t *testing.T) {
	server, _ := testServer()
	w := httptest.NewRecorder()
	server.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/health/ready", nil))
	testkit.ValidateHTTP(t, "Health", w.Body.Bytes())
	server.Handle("GET /api/v1/panic", false, false, func(http.ResponseWriter, *http.Request, auth.Session) error {
		panic("private panic details")
	})
	w = httptest.NewRecorder()
	server.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/panic", nil))
	if w.Code != 503 || strings.Contains(w.Body.String(), "private") {
		t.Fatal("panic was not contained")
	}
	cookie, _ := login(t, server)
	server.HandleStream("GET /api/v1/stream", func(w http.ResponseWriter, r *http.Request, _ auth.Session) error {
		if _, deadline := r.Context().Deadline(); deadline {
			t.Error("stream inherited control deadline")
		}
		_, _ = w.Write([]byte("AUDIO"))
		return domain.Fail("TEMPORARY_FAILURE", "Stream interrupted")
	})
	w = httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/api/v1/stream", nil)
	r.AddCookie(cookie)
	defer func() {
		if recover() != http.ErrAbortHandler || w.Body.String() != "AUDIO" {
			t.Fatal("stream error must abort without appending JSON")
		}
	}()
	server.ServeHTTP(w, r)
}
