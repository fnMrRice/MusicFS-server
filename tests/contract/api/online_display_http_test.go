package api_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/online"
	"github.com/fnMrRice/MusicFS-server/src/onlinehost"
	"github.com/fnMrRice/MusicFS-server/src/sourcedisplay"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestOnlineDisplayHTTPKindsBatchAndSourcePermissions(t *testing.T) {
	server, store, _ := libraryHTTP(t)
	cookie, session := login(t, server)
	runtimes := []onlinehost.Runtime{}
	for _, id := range []string{"albums", "artists", "playlists", "batch", "detail", "albumdetail", "artistdetail", "albumtracks", "artistalbums", "artisttracks"} {
		client, err := online.New(online.Netease, online.Scope{UserID: "admin", InstanceID: id}, online.Options{Transport: testkit.OnlineAccountTransport(onlineTransport{})})
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
		w := request("GET", "/online-instances/"+kind+"/search?q=Synthetic&kind="+kind, "", true)
		if w.Code != 200 {
			t.Fatal(kind, w.Code, w.Body.String())
		}
		testkit.ValidateHTTP(t, "OnlineSearchPage", w.Body.Bytes())
		var page online.SearchPage
		if json.Unmarshal(w.Body.Bytes(), &page) != nil || page.Kind != kind || len(page.Tracks) != 0 {
			t.Fatal("wrong result kind", w.Body.String())
		}
	}
	for _, tc := range []struct{ path, schema string }{{"albumdetail/albums/91", "OnlineAlbum"}, {"artistdetail/artists/92", "OnlineArtist"}, {"albumtracks/albums/91/tracks?limit=2", "OnlineSearchPage"}, {"artistalbums/artists/92/albums?limit=2", "OnlineSearchPage"}, {"artisttracks/artists/92/tracks?limit=2", "OnlineSearchPage"}} {
		w := request("GET", "/online-instances/"+tc.path, "", true)
		if w.Code != 200 {
			t.Fatal(tc.path, w.Code, w.Body.String())
		}
		testkit.ValidateHTTP(t, tc.schema, w.Body.Bytes())
	}
	w := request("POST", "/online-instances/batch/tracks/lookup", `{"externalIds":["42","43","42"]}`, true)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	testkit.ValidateHTTP(t, "OnlineTrackBatch", w.Body.Bytes())
	w = request("GET", "/online-instances/detail/playlists/91/tracks?limit=2", "", true)
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
		{"POST", "/online-instances/batch/tracks/lookup", `{"externalIds":["42"]}`, false, 403},
		{"POST", "/online-instances/other/tracks/lookup", `{"externalIds":["42"]}`, true, 404},
		{"GET", "/online-instances/albums/search?q=x&kind=unknown", "", true, 400},
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
