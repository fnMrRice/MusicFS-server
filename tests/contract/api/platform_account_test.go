package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/online"
	"github.com/fnMrRice/MusicFS-server/src/onlinehost"
)

type accountFake struct{ *online.Client }

func (p accountFake) PlatformDeveloperToken() string { return "" }
func (p accountFake) PlatformAccount(_ context.Context, r online.AccountRequest) (online.AccountView, error) {
	if r.Action == "begin" {
		return online.AccountView{}, domain.Fail("AUTH_REQUIRED", "Platform login required")
	}
	return online.AccountView{State: "signedOut"}, nil
}
func TestPlatformAccountHTTPIsolationAndCSRF(t *testing.T) {
	server, _, _ := libraryHTTP(t)
	cookie, session := login(t, server)
	c, err := online.New(online.Netease, online.Scope{UserID: "admin", InstanceID: "net"}, online.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	service, err := onlinehost.New([]onlinehost.Runtime{{Owner: "admin", InstanceID: "net", Name: "Netease", Platform: online.Netease, Enabled: true, ConfigRevision: "1", Provider: accountFake{c}}, {Owner: "other", InstanceID: "private", Platform: online.Netease, Enabled: true, ConfigRevision: "1", Provider: accountFake{c}}})
	if err != nil {
		t.Fatal(err)
	}
	server.RegisterOnline(service)
	for _, tc := range []struct {
		method, path, body string
		csrf               bool
		status             int
	}{
		{"GET", "net/account", "", false, 200},
		{"GET", "private/account", "", false, 404},
		{"POST", "net/account", `{"action":"logout"}`, false, 403},
		{"POST", "net/account", `{"action":"restore","secret":{}}`, true, 400},
		{"POST", "net/account", `{"action":"signalCancel"}`, true, 400},
		{"POST", "net/account", `{"action":"begin"}`, true, 403},
		{"POST", "net/account", `{"action":"logout"}`, true, 200},
	} {
		r := httptest.NewRequest(tc.method, "/api/v1/online-instances/"+tc.path, strings.NewReader(tc.body))
		r.AddCookie(cookie)
		r.Header.Set("Origin", origin)
		r.Header.Set("Content-Type", "application/json")
		if tc.csrf {
			r.Header.Set("X-CSRF-Token", session.CSRFToken)
		}
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("%s %s: %d", tc.method, tc.path, w.Code)
		}
		if strings.Contains(w.Body.String(), `"secret"`) {
			t.Fatal("private snapshot leaked over HTTP")
		}
	}
	r := httptest.NewRequest(http.MethodGet, "/api/v1/auth/session", nil)
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	server.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal("platform login failure ended MusicFS session")
	}
}
