package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	onlineplugins "github.com/fnMrRice/MusicFS-server/plugins/source/online"
	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/src/plugins/onlinehost"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

type onlineTransport struct{}

func (onlineTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	status, raw := 200, `{"code":200,"result":{"songs":[{"id":42,"name":"Synthetic","artists":[],"album":{"name":"Album"},"duration":120000}],"songCount":2,"hasMore":true}}`
	switch r.URL.Path {
	case "/api/v1/album/91":
		raw = `{"code":200,"album":{"id":91,"name":"Synthetic album","size":3},"songs":[{"id":42,"name":"Synthetic","ar":[],"al":{},"dt":120000}]}`
	case "/api/artist/92":
		raw = `{"code":200,"artist":{"id":92,"name":"Synthetic artist","albumSize":2,"musicSize":3}}`
	case "/api/artist/albums/92":
		raw = `{"code":200,"artist":{"id":92,"name":"Synthetic artist"},"hotAlbums":[{"id":91,"name":"Synthetic album","size":3}],"more":false}`
	case "/api/v1/artist/songs":
		raw = `{"code":200,"songs":[{"id":42,"name":"Synthetic","artists":[],"album":{},"duration":120000}],"more":false,"total":1}`
	case "/api/search/get":
		switch r.URL.Query().Get("type") {
		case "10":
			raw = `{"code":200,"result":{"albums":[{"id":91,"name":"Synthetic album","artists":[{"name":"Artist"}],"size":3}],"albumCount":1}}`
		case "100":
			raw = `{"code":200,"result":{"artists":[{"id":92,"name":"Synthetic artist","albumSize":1,"musicSize":3}],"artistCount":1}}`
		case "1000":
			raw = `{"code":200,"result":{"playlists":[{"id":93,"name":"Synthetic playlist","creator":{"nickname":"Creator"},"trackCount":3}],"playlistCount":1}}`
		}
	case "/api/v3/song/detail":
		raw = `{"code":200,"songs":[{"id":42,"name":"Synthetic","ar":[],"al":{"name":"Album","picUrl":"http://p2.music.126.net/song.jpg"},"dt":120000}]}`
	case "/api/personalized/playlist":
		raw = `{"code":200,"result":[{"id":91,"name":"Synthetic","copywriter":"Public","trackCount":3}]}`
	case "/api/v3/discovery/recommend/songs":
		songs := make([]string, online.MaxPageItems)
		for i := range songs {
			songs[i] = fmt.Sprintf(`{"id":%d,"name":"Daily track","ar":[],"al":{"name":"Album","picUrl":"https://p2.music.126.net/daily.jpg"},"dt":120000}`, i+1)
		}
		raw = `{"code":200,"data":{"dailySongs":[` + strings.Join(songs, ",") + `]}}`
	case "/api/user/playlist":
		raw = `{"code":200,"more":false,"playlist":[{"id":91,"name":"Account playlist","trackCount":3}]}`
	case "/api/v6/playlist/detail":
		raw = `{"code":200,"playlist":{"id":91,"name":"Synthetic","privacy":0,"trackCount":3,"trackIds":[{"id":42},{"id":42},{"id":43}]}}`
	case "/soso/fcgi-bin/client_search_cp":
		status, raw = 500, "private upstream diagnostic"
	}
	return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(raw))}, nil
}

func TestOnlineHTTPPrivateAuthenticatedMetadataAndErrors(t *testing.T) {
	server, store, _ := libraryHTTP(t)
	cookie, session := login(t, server)
	runtimes := []onlinehost.Runtime{}
	for _, item := range []struct{ owner, id, platform string }{{"admin", "7fe223d0-a3bd-5e87-87f8-305dc26d9041", online.Netease}, {"admin", "db019d81-1fbb-528c-a9f7-9c47006844a2", online.Netease}, {"admin", "b5e86bbe-524b-53fc-bde9-bbe3cbb02a80", online.QQMusic}, {"admin", "2e2b7962-89d2-5815-9804-0d395497f906", online.AppleMusic}, {"other", "843558e1-a158-56ac-834a-baffd12bb5c5", online.Netease}} {
		provider, err := onlineplugins.New(item.platform, online.Scope{UserID: item.owner, InstanceID: item.id}, onlineplugins.Options{Transport: testkit.OnlineAccountTransport(onlineTransport{})})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(provider.Close)
		if item.platform == online.Netease {
			testkit.RestoreOnlineFixture(t, provider, item.platform, item.owner, item.id)
		}
		var runtimeProvider onlinehost.Provider = provider
		if item.id == "db019d81-1fbb-528c-a9f7-9c47006844a2" {
			runtimeProvider = dailyTracksFixture{provider}
		}
		runtimes = append(runtimes, onlinehost.Runtime{Owner: item.owner, InstanceID: item.id, Name: item.id, Platform: item.platform, ConfigRevision: "private-config", Enabled: true, Provider: runtimeProvider})
	}
	runtimes = append(runtimes, onlinehost.Runtime{Owner: "admin", InstanceID: "9d986e6a-04fd-522e-b5b5-eb9f28ac4ce6", Name: "Disabled", Platform: online.Netease, ConfigRevision: "config"})
	service, err := onlinehost.New(runtimes)
	if err != nil {
		t.Fatal(err)
	}
	server.RegisterOnline(service)
	request := func(method, path, body string, authorized bool, mutate func(*http.Request)) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, "/api/v1"+path, strings.NewReader(body))
		if authorized {
			r.AddCookie(cookie)
		}
		r.Header.Set("Origin", origin)
		r.Header.Set("X-CSRF-Token", session.CSRFToken)
		r.Header.Set("Content-Type", "application/json")
		if mutate != nil {
			mutate(r)
		}
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		for _, private := range []string{"private-config", "private upstream diagnostic", `"userId"`, `"executable"`, `"assetId"`, `"streamUrl"`} {
			if strings.Contains(w.Body.String(), private) {
				t.Fatalf("online response leaked %s", private)
			}
		}
		return w
	}
	w := request("GET", "/online-instances", "", true, nil)
	var instances struct {
		Items []onlinehost.Instance `json:"items"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &instances) != nil || len(instances.Items) != 5 {
		t.Fatalf("instances: %d %s", w.Code, w.Body.String())
	}
	testkit.ValidateHTTP(t, "OnlineInstances", w.Body.Bytes())
	for _, item := range instances.Items {
		for _, cap := range item.Capabilities {
			if item.Platform != online.Netease && cap.Supported {
				t.Fatal("unverified capability advertised")
			}
		}
	}
	w = request("GET", "/online-instances/7fe223d0-a3bd-5e87-87f8-305dc26d9041/search?q=Synthetic&limit=1", "", true, nil)
	var page online.SearchPage
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &page) != nil || page.NextCursor == nil || page.Tracks[0].PlaybackAvailability != "unknown" {
		t.Fatalf("search: %d %s", w.Code, w.Body.String())
	}
	testkit.ValidateHTTP(t, "OnlineSearchPage", w.Body.Bytes())
	for _, tc := range []struct {
		path string
		code int
	}{
		{"/online-instances/843558e1-a158-56ac-834a-baffd12bb5c5/search?q=Synthetic", 404},
		{"/online-instances/9d986e6a-04fd-522e-b5b5-eb9f28ac4ce6/search?q=Synthetic", 503},
		{"/online-instances/2e2b7962-89d2-5815-9804-0d395497f906/search?q=Synthetic", 403},
		{"/online-instances/b5e86bbe-524b-53fc-bde9-bbe3cbb02a80/search?q=Synthetic", 403},
		{"/online-instances/7fe223d0-a3bd-5e87-87f8-305dc26d9041/search?q=Synthetic&limit=1", 429},
		{"/online-instances/7fe223d0-a3bd-5e87-87f8-305dc26d9041/search?q=Changed&limit=1&cursor=" + url.QueryEscape(*page.NextCursor), 400},
		{"/online-instances/db019d81-1fbb-528c-a9f7-9c47006844a2/search?q=Synthetic&limit=1&cursor=" + url.QueryEscape(*page.NextCursor), 400},
		{"/online-instances/7fe223d0-a3bd-5e87-87f8-305dc26d9041/search?q=Synthetic&url=https://private.invalid", 400},
		{"/online-instances/7fe223d0-a3bd-5e87-87f8-305dc26d9041/search?q=Synthetic&q=Again", 400},
		{"/online-instances/7fe223d0-a3bd-5e87-87f8-305dc26d9041/search?q=Synthetic&limit=01", 400},
		{"/online-instances/7fe223d0-a3bd-5e87-87f8-305dc26d9041/search?q=Synthetic&limit=51", 400},
		{"/online-instances/7fe223d0-a3bd-5e87-87f8-305dc26d9041/search?q=", 400},
		{"/online-instances/7fe223d0-a3bd-5e87-87f8-305dc26d9041/tracks/42?accountId=private", 400},
		{"/online-instances/7fe223d0-a3bd-5e87-87f8-305dc26d9041/recommendations?cursor=invalid", 400},
		{"/online-instances/7fe223d0-a3bd-5e87-87f8-305dc26d9041/daily-songs?limit=0", 400},
		{"/online-instances/7fe223d0-a3bd-5e87-87f8-305dc26d9041/daily-songs?limit=51", 400},
		{"/online-instances/7fe223d0-a3bd-5e87-87f8-305dc26d9041/daily-songs?limit=1&limit=2", 400},
		{"/online-instances/7fe223d0-a3bd-5e87-87f8-305dc26d9041/daily-songs?accountId=other", 400},
		{"/online-instances/843558e1-a158-56ac-834a-baffd12bb5c5/daily-songs", 404},
		{"/online-instances/b5e86bbe-524b-53fc-bde9-bbe3cbb02a80/daily-songs", 403},
	} {
		w = request("GET", tc.path, "", true, nil)
		if w.Code != tc.code {
			t.Fatalf("%s: %d %s", tc.path, w.Code, w.Body.String())
		}
		testkit.ValidateHTTP(t, "Error", w.Body.Bytes())
		if w.Code == 429 && w.Header().Get("Retry-After") == "" {
			t.Fatal("rate limit lost retry hint")
		}
	}
	for _, tc := range []struct {
		path, schema string
		dailyTracks  int
	}{
		{"/tracks/42", "OnlineTrack", 0},
		{"/recommendations?limit=1", "OnlineRecommendationPage", 0},
		{"/playlists/91", "OnlineObservation", 0},
		{"/playlists/91/tracks?limit=1", "OnlinePlaylistTracksPage", 0},
		{"/daily-songs", "OnlineDailySongsPage", online.MaxPageItems},
		{"/daily-songs?limit=2", "OnlineDailySongsPage", 2},
	} {
		time.Sleep(1050 * time.Millisecond)
		instance := "7fe223d0-a3bd-5e87-87f8-305dc26d9041"
		if tc.dailyTracks > 0 {
			instance = "db019d81-1fbb-528c-a9f7-9c47006844a2"
		}
		w = request("GET", "/online-instances/"+instance+tc.path, "", true, nil)
		if w.Code != 200 {
			t.Fatalf("online read: %d %s", w.Code, w.Body.String())
		}
		testkit.ValidateHTTP(t, tc.schema, w.Body.Bytes())
		if tc.schema == "OnlineRecommendationPage" {
			var recs online.RecommendationPage
			if err := json.Unmarshal(w.Body.Bytes(), &recs); err != nil || len(recs.Items) != 1 || !recs.Personalized {
				t.Fatalf("missing account recommendation: %s", w.Body)
			}
			playlist := recs.Items[0].Playlist
			if playlist.ExternalID != "daily-songs:12345" || playlist.Writable || playlist.TrackCount == nil || *playlist.TrackCount != online.MaxPageItems || playlist.CoverURL == nil || *playlist.CoverURL != "https://p2.music.126.net/daily.jpg" {
				t.Fatalf("invalid daily playlist card: %+v", playlist)
			}
		}
		if tc.schema == "OnlinePlaylistTracksPage" {
			var page online.PlaylistTracksPage
			if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil || len(page.Items) != 1 || page.Items[0].Track == nil || page.Items[0].Track.CoverURL == nil || *page.Items[0].Track.CoverURL != "https://p2.music.126.net/song.jpg" {
				t.Fatalf("playlist HTTP response lost track artwork: %s", w.Body)
			}
		}
		if tc.dailyTracks > 0 {
			var daily online.DailySongsPage
			if err := json.Unmarshal(w.Body.Bytes(), &daily); err != nil || len(daily.Tracks) != tc.dailyTracks || daily.ObservedAt.IsZero() || daily.Tracks[0].ExternalID != "1" || daily.Tracks[1].ExternalID != "2" {
				t.Fatalf("invalid daily song page, want %d ordered tracks: %v", tc.dailyTracks, err)
			}
		}
		if tc.schema == "OnlineObservation" {
			var observation online.PlaylistObservation
			if json.Unmarshal(w.Body.Bytes(), &observation) != nil || !observation.Complete || len(observation.Entries) != 3 || observation.Entries[0].ExternalTrackID != observation.Entries[1].ExternalTrackID {
				t.Fatal("duplicate playlist occurrences were lost")
			}
		}
	}
	for _, tc := range []struct {
		method, path, body string
		auth               bool
		modify             func(*http.Request)
		code               int
	}{
		{"GET", "/online-instances", "", false, nil, 401},
		{"GET", "/online-instances/843558e1-a158-56ac-834a-baffd12bb5c5/playlists/daily-songs:12345/tracks", "", true, nil, 404},
		{"GET", "/online-instances/7fe223d0-a3bd-5e87-87f8-305dc26d9041/playlists/daily-songs:67890/tracks", "", true, nil, 404},
		{"GET", "/online-instances/7fe223d0-a3bd-5e87-87f8-305dc26d9041/daily-songs", "", false, nil, 401},
		{"POST", "/online-instances/7fe223d0-a3bd-5e87-87f8-305dc26d9041/tracks/42/resolve", "{}", true, nil, 415},
		{"POST", "/online-instances/7fe223d0-a3bd-5e87-87f8-305dc26d9041/tracks/42/resolve", `{"url":"private"}`, true, nil, 400},
		{"POST", "/online-instances/7fe223d0-a3bd-5e87-87f8-305dc26d9041/tracks/42/resolve", "{}", true, func(r *http.Request) { r.Header.Del("X-CSRF-Token") }, 403},
		{"POST", "/online-instances/7fe223d0-a3bd-5e87-87f8-305dc26d9041/tracks/42/resolve", "{}", true, func(r *http.Request) { r.Header.Set("Origin", "https://other.invalid") }, 403},
	} {
		if w := request(tc.method, tc.path, tc.body, tc.auth, tc.modify); w.Code != tc.code {
			t.Fatalf("auth/resolve: %d %s", w.Code, w.Body.String())
		}
	}
	const dailyPath = "/online-instances/7fe223d0-a3bd-5e87-87f8-305dc26d9041/playlists/daily-songs:12345/tracks?limit=2"
	next := ""
	for offset := 0; offset < 4; offset += 2 {
		time.Sleep(1050 * time.Millisecond)
		path := dailyPath
		if next != "" {
			path += "&cursor=" + url.QueryEscape(next)
		}
		w = request("GET", path, "", true, nil)
		if w.Code != 200 {
			t.Fatalf("daily playlist tracks: %d %s", w.Code, w.Body)
		}
		testkit.ValidateHTTP(t, "OnlinePlaylistTracksPage", w.Body.Bytes())
		var page online.PlaylistTracksPage
		if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil || page.Playlist.ExternalID != "daily-songs:12345" || len(page.Items) != 2 || page.NextCursor == nil || *page.NextCursor == next {
			t.Fatalf("invalid daily playlist page: %s", w.Body)
		}
		for i, item := range page.Items {
			if item.Position != offset+i || item.Track == nil || item.Track.ExternalID != fmt.Sprint(offset+i+1) {
				t.Fatalf("daily tracks lost ordering or metadata: %+v", item)
			}
		}
		next = *page.NextCursor
	}
	tracks, err := store.ListTracks(context.Background(), "admin", "", "", 10)
	if err != nil || len(tracks.Items) != 0 {
		t.Fatal("public metadata implicitly imported audio assets", err)
	}
}
