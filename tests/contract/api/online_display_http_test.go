package api_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	onlineplugins "github.com/fnMrRice/MusicFS-server/plugins/source/online"
	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/sourcedisplay"
	"github.com/fnMrRice/MusicFS-server/src/plugins/onlinehost"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestOnlineDisplayHTTPKindsBatchAndSourcePermissions(t *testing.T) {
	server, store, _ := libraryHTTP(t)
	cookie, session := login(t, server)
	runtimes := []onlinehost.Runtime{}
	for _, label := range []string{"albums", "artists", "playlists", "batch", "detail", "albumdetail", "artistdetail", "albumtracks", "artistalbums", "artisttracks"} {
		id := fixtureInstanceID(label)
		client, err := onlineplugins.New(online.Netease, online.Scope{UserID: "admin", InstanceID: id}, onlineplugins.Options{Transport: testkit.OnlineAccountTransport(onlineTransport{})})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(client.Close)
		testkit.RestoreOnlineFixture(t, client, online.Netease, "admin", id)
		runtimes = append(runtimes, onlinehost.Runtime{Owner: "admin", InstanceID: id, Name: id, Platform: online.Netease, ConfigRevision: "revision", Enabled: true, Provider: client})
	}
	service, err := onlinehost.New(runtimes)
	if err != nil {
		t.Fatal(err)
	}
	server.RegisterOnline(service)
	server.RegisterSourceDisplay(sourcedisplay.New(store, service))
	request := func(method, path, body string, csrf bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/api/v1"+path, strings.NewReader(body))
		r.AddCookie(cookie)
		r.Header.Set("Origin", origin)
		r.Header.Set("Content-Type", "application/json")
		if csrf {
			r.Header.Set("X-CSRF-Token", session.CSRFToken)
		}
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		return w
	}
	for _, kind := range []string{"albums", "artists", "playlists"} {
		w := request("GET", "/online-instances/"+fixtureInstanceID(kind)+"/search?q=Synthetic&kind="+kind, "", true)
		if w.Code != 200 {
			t.Fatal(kind, w.Code, w.Body.String())
		}
		testkit.ValidateHTTP(t, "OnlineSearchPage", w.Body.Bytes())
		var page online.SearchPage
		if json.Unmarshal(w.Body.Bytes(), &page) != nil || page.Kind != kind || len(page.Tracks) != 0 {
			t.Fatal("wrong result kind", w.Body.String())
		}
	}
	for _, tc := range []struct{ path, schema string }{{"db30b386-20a6-57d4-b151-88e69c4d5efa/albums/91", "OnlineAlbum"}, {"28d25374-8044-55fe-a377-d909fcc82bb7/artists/92", "OnlineArtist"}, {"256c5302-5381-53c9-a138-43c402d64248/albums/91/tracks?limit=2", "OnlineSearchPage"}, {"3caef4d5-8ec3-5f2c-8988-2eea0cd1991a/artists/92/albums?limit=2", "OnlineSearchPage"}, {"bdae1994-6dec-50bd-960b-1d2ea1a17f7f/artists/92/tracks?limit=2", "OnlineSearchPage"}} {
		w := request("GET", "/online-instances/"+tc.path, "", true)
		if w.Code != 200 {
			t.Fatal(tc.path, w.Code, w.Body.String())
		}
		testkit.ValidateHTTP(t, tc.schema, w.Body.Bytes())
	}
	w := request("POST", "/online-instances/bddd87bf-5609-5190-9f7d-f43ff6223f18/tracks/lookup", `{"externalIds":["42","43","42"]}`, true)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	testkit.ValidateHTTP(t, "OnlineTrackBatch", w.Body.Bytes())
	w = request("GET", "/online-instances/e2cdfdf4-1580-52b8-81d2-e3d1d0b457c0/playlists/91/tracks?limit=2", "", true)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	testkit.ValidateHTTP(t, "OnlinePlaylistTracksPage", w.Body.Bytes())
	w = request("POST", "/source-refs/lookup", `{"sources":[{"sourceTrackId":"`+domain.NewID()+`","remoteSourceId":null}]}`, true)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	testkit.ValidateHTTP(t, "SourceDisplayPage", w.Body.Bytes())
	if !strings.Contains(w.Body.String(), "NOT_FOUND") {
		t.Fatal("missing source not explicit")
	}
	for _, tc := range []struct {
		method, path, body string
		csrf               bool
		code               int
	}{
		{"POST", "/source-refs/lookup", `{"sources":[]}`, true, 400},
		{"POST", "/source-refs/lookup", `{"sources":[]}`, false, 403},
		{"POST", "/online-instances/bddd87bf-5609-5190-9f7d-f43ff6223f18/tracks/lookup", `{"externalIds":["42"]}`, false, 403},
		{"POST", "/online-instances/e4fe4d4f-f95b-57a9-8c27-aec99a401f05/tracks/lookup", `{"externalIds":["42"]}`, true, 404},
		{"GET", "/online-instances/c30c766a-3e82-569a-a016-cb973a70926e/search?q=x&kind=unknown", "", true, 400},
	} {
		w := request(tc.method, tc.path, tc.body, tc.csrf)
		if w.Code != tc.code {
			t.Fatal(tc.path, w.Code, w.Body.String())
		}
		testkit.ValidateHTTP(t, "Error", w.Body.Bytes())
	}
	tracks, err := store.ListTracks(context.Background(), "admin", "", "", 10)
	if err != nil || len(tracks.Items) != 0 {
		t.Fatal("display implicitly imported tracks", err)
	}
}
