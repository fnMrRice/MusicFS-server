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
	c, err := online.New(online.Netease, online.Scope{UserID: "admin", InstanceID: "7fe223d0-a3bd-5e87-87f8-305dc26d9041"}, online.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	service, err := onlinehost.New([]onlinehost.Runtime{{Owner: "admin", InstanceID: "7fe223d0-a3bd-5e87-87f8-305dc26d9041", Name: "Netease", Platform: online.Netease, Enabled: true, ConfigRevision: "1", Provider: accountFake{c}}, {Owner: "other", InstanceID: "843558e1-a158-56ac-834a-baffd12bb5c5", Platform: online.Netease, Enabled: true, ConfigRevision: "1", Provider: accountFake{c}}})
	if err != nil {
		t.Fatal(err)
	}
	server.RegisterOnline(service)
	for _, tc := range []struct {
		method, path, body string
		csrf               bool
		status             int
	}{
		{"GET", "7fe223d0-a3bd-5e87-87f8-305dc26d9041/account", "", false, 200},
		{"GET", "7fe223d0-a3bd-5e87-87f8-305dc26d9041/search?q=sample", "", false, 403},
		{"GET", "7fe223d0-a3bd-5e87-87f8-305dc26d9041/tracks/42", "", false, 403},
		{"GET", "7fe223d0-a3bd-5e87-87f8-305dc26d9041/playlists/42", "", false, 403},
		{"GET", "7fe223d0-a3bd-5e87-87f8-305dc26d9041/recommendations?limit=1", "", false, 403},
		{"POST", "7fe223d0-a3bd-5e87-87f8-305dc26d9041/tracks/42/resolve", "{}", true, 403},
		{"GET", "843558e1-a158-56ac-834a-baffd12bb5c5/account", "", false, 404},
		{"POST", "7fe223d0-a3bd-5e87-87f8-305dc26d9041/account", `{"action":"logout"}`, false, 403},
		{"POST", "7fe223d0-a3bd-5e87-87f8-305dc26d9041/account", `{"action":"restore","secret":{}}`, true, 400},
		{"POST", "7fe223d0-a3bd-5e87-87f8-305dc26d9041/account", `{"action":"signalCancel"}`, true, 400},
		{"POST", "7fe223d0-a3bd-5e87-87f8-305dc26d9041/account", `{"action":"begin"}`, true, 403},
		{"POST", "7fe223d0-a3bd-5e87-87f8-305dc26d9041/account", `{"action":"logout"}`, true, 200},
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
		if tc.status == 403 && tc.method == "GET" && (!strings.Contains(w.Body.String(), "PLATFORM_AUTH_REQUIRED") || w.Header().Get("WWW-Authenticate") != "") {
			t.Fatal("platform gate used MusicFS session authentication")
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
