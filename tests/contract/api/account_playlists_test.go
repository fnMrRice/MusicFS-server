package api_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/online"
	"github.com/fnMrRice/MusicFS-server/src/onlinehost"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestAccountPlaylistsHTTPScopesAndContract(t *testing.T) {
	server, _, _ := libraryHTTP(t)
	cookie, _ := login(t, server)
	client, err := online.New(online.Netease, online.Scope{UserID: "admin", InstanceID: "net"}, online.Options{Transport: testkit.OnlineAccountTransport(onlineTransport{})})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	testkit.RestoreOnlineFixture(t, client, online.Netease, "admin", "net")
	service, err := onlinehost.New([]onlinehost.Runtime{{Owner: "admin", InstanceID: "net", Platform: online.Netease, ConfigRevision: "r1", Enabled: true, Provider: client}})
	if err != nil {
		t.Fatal(err)
	}
	server.RegisterOnline(service)
	request := func(path string, authenticated bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/api/v1/online-instances/"+path, nil)
		if authenticated {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		return w
	}
	w := request("net/playlists?limit=20", true)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	testkit.ValidateHTTP(t, "OnlinePlaylistsPage", w.Body.Bytes())
	var page online.PlaylistsPage
	if json.Unmarshal(w.Body.Bytes(), &page) != nil || len(page.Items) != 1 || page.Items[0].Title != "Account playlist" || page.Consistency != online.Unversioned {
		t.Fatal("invalid account playlist response", w.Body.String())
	}
	for _, tc := range []struct {
		path          string
		authenticated bool
		status        int
	}{
		{"net/playlists", false, 401},
		{"other/playlists", true, 404},
		{"net/playlists?userId=other", true, 400},
		{"net/playlists?url=https://example.com", true, 400},
		{"net/playlists?limit=1&limit=2", true, 400},
		{"net/playlists?limit=51", true, 400},
		{"net/playlists?limit=01", true, 400},
		{"net/playlists?cursor=invalid", true, 400},
	} {
		if w := request(tc.path, tc.authenticated); w.Code != tc.status {
			t.Fatalf("%s: %d %s", tc.path, w.Code, w.Body.String())
		}
	}
	if _, err := client.Account(context.Background(), online.AccountRequest{Action: "logout"}); err != nil {
		t.Fatal(err)
	}
	if w := request("net/playlists", true); w.Code != 403 {
		t.Fatal("signed-out account accepted", w.Code, w.Body.String())
	}
}
