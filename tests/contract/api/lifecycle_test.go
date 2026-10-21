package api_test

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/app/api"
	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/core/lifecycle"
)

func lifecycleAPI(t *testing.T) (*api.Server, *lifecycle.Manager) {
	t.Helper()
	m, err := lifecycle.New(lifecycle.Options{Mode: lifecycle.ClientManaged, InstanceID: "test-instance"})
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Activate(); err != nil {
		t.Fatal(err)
	}
	a := auth.New(&memoryAuth{sessions: map[string]auth.Session{}}, testToken, []string{origin}, time.Hour)
	a.OnRevoke = m.RevokeSession
	s := api.New(api.Options{Auth: a, InstanceID: "test-instance", Ready: m.Ready, SecureCookie: true, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	s.RegisterLifecycle(m)
	return s, m
}
func lifecycleRequest(s *api.Server, cookie *http.Cookie, session auth.Session, path, body, key, remote string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	r.RemoteAddr = remote
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", origin)
	r.Header.Set("X-CSRF-Token", session.CSRFToken)
	r.Header.Set("Idempotency-Key", key)
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}
func TestLifecycleHTTPAuthInstanceSequenceAndRevocation(t *testing.T) {
	s, m := lifecycleAPI(t)
	cookie, a := login(t, s)
	other, b := login(t, s)
	create := `{"instanceId":"test-instance","clientId":"client-first"}`
	w := lifecycleRequest(s, cookie, a, "/api/v1/lifecycle/leases", create, "operation-first", "127.0.0.1:1234")
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var lease lifecycle.Lease
	if err := json.Unmarshal(w.Body.Bytes(), &lease); err != nil {
		t.Fatal(err)
	}
	w = lifecycleRequest(s, other, b, "/api/v1/lifecycle/leases", `{"instanceId":"test-instance","clientId":"client-other"}`, "operation-second", "[::1]:1234")
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	renew := "/api/v1/lifecycle/leases/" + lease.ID + "/renew"
	w = lifecycleRequest(s, other, b, renew, `{"instanceId":"test-instance","sequence":1}`, "", "127.0.0.1:1234")
	if w.Code != 404 {
		t.Fatal("another session renewed lease", w.Code)
	}
	w = lifecycleRequest(s, cookie, a, renew, `{"instanceId":"test-instance","sequence":1}`, "", "127.0.0.1:1234")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	first := append([]byte{}, w.Body.Bytes()...)
	w = lifecycleRequest(s, cookie, a, renew, `{"instanceId":"test-instance","sequence":1}`, "", "127.0.0.1:1234")
	if w.Code != 200 || !bytes.Equal(first, w.Body.Bytes()) {
		t.Fatal("renew replay mutated expiry", w.Body.String())
	}
	w = lifecycleRequest(s, cookie, a, renew, `{"instanceId":"old-instance","sequence":2}`, "", "127.0.0.1:1234")
	if w.Code != 409 {
		t.Fatal("old process identity was accepted", w.Code)
	}
	r := httptest.NewRequest(http.MethodDelete, "/api/v1/auth/session", nil)
	r.AddCookie(cookie)
	r.Header.Set("Origin", origin)
	r.Header.Set("X-CSRF-Token", a.CSRFToken)
	result := httptest.NewRecorder()
	s.ServeHTTP(result, r)
	if result.Code != 204 || m.Status().ActiveLeases != 1 {
		t.Fatal("logout did not revoke exactly one session")
	}
	w = lifecycleRequest(s, cookie, a, renew, `{"instanceId":"test-instance","sequence":2}`, "", "127.0.0.1:1234")
	if w.Code != 401 {
		t.Fatal("revoked session was accepted", w.Code)
	}
}

func TestLifecycleHTTPRejectsRemoteCSRFNullAndDrainingAcquire(t *testing.T) {
	s, m := lifecycleAPI(t)
	cookie, session := login(t, s)
	for _, body := range []string{`{}`, `{"instanceId":null,"clientId":"client-one"}`, `{"instanceId":"test-instance","clientId":null}`, `{"instanceId":"test-instance","clientId":"client-one","startupMode":"standalone"}`} {
		w := lifecycleRequest(s, cookie, session, "/api/v1/lifecycle/leases", body, "operation-first", "127.0.0.1:1234")
		if w.Code != 400 {
			t.Fatal("invalid lifecycle body accepted", w.Code, body)
		}
	}
	body := `{"instanceId":"test-instance","clientId":"client-one"}`
	for _, remote := range []string{"192.0.2.4:1234", "invalid", "[2001:db8::1]:1234"} {
		w := lifecycleRequest(s, cookie, session, "/api/v1/lifecycle/leases", body, "operation-first", remote)
		if w.Code != 403 {
			t.Fatal("nonlocal lease accepted", remote, w.Code)
		}
	}
	bad := session
	bad.CSRFToken = "wrong"
	w := lifecycleRequest(s, cookie, bad, "/api/v1/lifecycle/leases", body, "operation-first", "127.0.0.1:1234")
	if w.Code != 403 {
		t.Fatal("CSRF bypassed")
	}
	w = lifecycleRequest(s, nil, session, "/api/v1/lifecycle/leases", body, "operation-first", "127.0.0.1:1234")
	if w.Code != 401 {
		t.Fatal("anonymous lease accepted")
	}
	w = lifecycleRequest(s, cookie, session, "/api/v1/lifecycle/leases", body, "operation-first", "127.0.0.1:1234")
	var lease lifecycle.Lease
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &lease) != nil {
		t.Fatal(w.Code, w.Body.String())
	}
	path := "/api/v1/lifecycle/leases/" + lease.ID + "/release"
	w = lifecycleRequest(s, cookie, session, path, `{"instanceId":"test-instance"}`, "", "127.0.0.1:1234")
	if w.Code != 200 || m.Status().State != "draining" {
		t.Fatal(w.Code, w.Body.String())
	}
	first := append([]byte{}, w.Body.Bytes()...)
	w = lifecycleRequest(s, cookie, session, path, `{"instanceId":"test-instance"}`, "", "127.0.0.1:1234")
	if w.Code != 200 || !reflect.DeepEqual(first, w.Body.Bytes()) {
		t.Fatal("release replay was rejected during draining")
	}
	w = lifecycleRequest(s, cookie, session, "/api/v1/lifecycle/leases", body, "operation-next", "127.0.0.1:1234")
	if w.Code != 503 {
		t.Fatal("draining server accepted a new lease")
	}
}
