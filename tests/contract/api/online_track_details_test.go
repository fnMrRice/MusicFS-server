package api_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	onlineplugins "github.com/fnMrRice/MusicFS-server/plugins/source/online"
	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/src/plugins/onlinehost"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestOnlineTrackDetailsHTTPContractAndOwnership(t *testing.T) {
	const instance = "7fe223d0-a3bd-5e87-87f8-305dc26d9041"
	client, err := onlineplugins.New(online.Netease, online.Scope{UserID: "admin", InstanceID: instance}, onlineplugins.Options{Transport: testkit.OnlineAccountTransport(onlineTransport{})})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	testkit.RestoreOnlineFixture(t, client, online.Netease, "admin", instance)
	service, err := onlinehost.New([]onlinehost.Runtime{{Owner: "admin", InstanceID: instance, Platform: online.Netease, ConfigRevision: "fixture", Enabled: true, Provider: client}})
	if err != nil {
		t.Fatal(err)
	}
	server, _ := testServer()
	server.RegisterOnline(service)
	cookie, _ := login(t, server)
	for _, tc := range []struct {
		path   string
		auth   bool
		status int
	}{
		{"/online-instances/" + instance + "/tracks/42/details", false, 401},
		{"/online-instances/843558e1-a158-56ac-834a-baffd12bb5c5/tracks/42/details", true, 404},
		{"/online-instances/" + instance + "/tracks/42/details?accountId=another", true, 400},
		{"/online-instances/" + instance + "/tracks/42/details", true, 200},
	} {
		r := httptest.NewRequest(http.MethodGet, "/api/v1"+tc.path, nil)
		if tc.auth {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatal(tc.path, w.Code, w.Body.String())
		}
		if tc.status == 200 {
			testkit.ValidateHTTP(t, "TrackDetails", w.Body.Bytes())
			if !strings.Contains(w.Body.String(), "Synthetic") || !strings.Contains(w.Body.String(), "\"sections\"") {
				t.Fatal("provider details did not reach HTTP", w.Body.String())
			}
		}
	}
}
