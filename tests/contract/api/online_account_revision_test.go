package api_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/src/onlinehost"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

type revisionOnlineProvider struct {
	onlinehost.Provider
	view online.AccountView
}

func (p *revisionOnlineProvider) PlatformSession(context.Context) (online.AccountView, error) {
	return p.view, nil
}

func TestOnlineHTTPAccountRevisionFollowsValidatedLogin(t *testing.T) {
	server, _, _ := libraryHTTP(t)
	cookie, _ := login(t, server)
	p := &revisionOnlineProvider{view: online.AccountView{State: "signedIn", AccountID: "12345", Revision: "login-1"}}
	r := onlinehost.Runtime{Owner: "admin", InstanceID: "7fe223d0-a3bd-5e87-87f8-305dc26d9041", Platform: online.Netease, ConfigRevision: "r1", Enabled: true, Provider: p}
	service, err := onlinehost.New([]onlinehost.Runtime{r})
	if err != nil {
		t.Fatal(err)
	}
	server.RegisterOnline(service)
	verify := func(want string) {
		t.Helper()
		request := httptest.NewRequest("GET", "/api/v1/online-instances", nil)
		request.AddCookie(cookie)
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		if response.Code != 200 {
			t.Fatal(response.Code, response.Body.String())
		}
		testkit.ValidateHTTP(t, "OnlineInstances", response.Body.Bytes())
		var payload struct {
			Items []map[string]any `json:"items"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil || len(payload.Items) != 1 {
			t.Fatal("invalid online instance payload", payload, err)
		}
		value, present := payload.Items[0]["accountRevision"]
		if want == "" && present || want != "" && value != want {
			t.Fatal("unexpected public account revision", payload.Items[0])
		}
		if want != "" && payload.Items[0]["accountId"] != "12345" {
			t.Fatal("validated account identity changed", payload.Items[0])
		}
	}
	verify("login-1")
	p.view.Revision = "login-2"
	verify("login-2")
	p.view.State = "signedOut"
	verify("")
	p.view.State = "signedIn"
	r.Enabled = false
	if err := service.UpdateRuntime(r); err != nil {
		t.Fatal(err)
	}
	verify("")
}
