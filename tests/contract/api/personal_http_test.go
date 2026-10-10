package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/auth"
	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/personal"
	"github.com/fnMrRice/MusicFS-server/src/playback"
	"github.com/fnMrRice/MusicFS-server/src/pluginproto"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestPersonalHTTPProfileFavoritesHistoryAndAuthorization(t *testing.T) {
	server, store, _ := libraryHTTP(t)
	server.RegisterPersonal(personal.New(store))
	cookie, session := login(t, server)
	ctx := context.Background()
	tracks := map[string]library.Track{}
	for _, owner := range []string{"admin", "other"} {
		provider := "mine"
		if owner == "other" {
			provider = "private"
		}
		if _, _, err := store.Enqueue(ctx, owner, provider, testkit.ScanPlan(provider), "personal-fixture"); err != nil {
			t.Fatal(err)
		}
		job, err := store.Claim(ctx)
		if err != nil {
			t.Fatal(err)
		}
		items := []library.Observation{}
		for _, name := range []string{"first", "second"} {
			items = append(items, library.Observation{Observation: pluginproto.Observation{ExternalID: name, RootID: provider, RelativePath: "secret/" + name + ".wav", DisplayName: name, ObservedVersion: "v1", SizeBytes: 100}, Artists: []string{}})
		}
		if err = store.Publish(ctx, job, testkit.ScanPublication(job, items)); err != nil {
			t.Fatal(err)
		}
		if err = store.Finish(ctx, job, len(items), nil); err != nil {
			t.Fatal(err)
		}
		page, err := store.ListTracks(ctx, owner, provider, "", 10)
		if err != nil {
			t.Fatal(err)
		}
		for _, track := range page.Items {
			tracks[owner+"/"+track.DisplayName] = track
		}
	}
	request := func(method, path string, body any, status int, change func(*http.Request)) *httptest.ResponseRecorder {
		t.Helper()
		var raw []byte
		if body != nil {
			raw, _ = json.Marshal(body)
		}
		r := httptest.NewRequest(method, "/api/v1"+path, bytes.NewReader(raw))
		r.AddCookie(cookie)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", origin)
		r.Header.Set("X-CSRF-Token", session.CSRFToken)
		if change != nil {
			change(r)
		}
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "secret/") || strings.Contains(w.Body.String(), tracks["other/first"].ID) {
			t.Fatal("personal response leaked private source")
		}
		return w
	}
	profile := func(w *httptest.ResponseRecorder) personal.Profile {
		t.Helper()
		var p personal.Profile
		if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
			t.Fatal(err)
		}
		return p
	}
	p := profile(request("GET", "/me/profile", nil, 200, nil))
	if p.User.ID != "admin" || p.Nickname != nil || p.Avatar != nil || p.AvatarUploadSupported {
		t.Fatal("profile invented user data")
	}
	p = profile(request("PUT", "/me/profile", map[string]any{"nickname": "  听歌的人  "}, 200, nil))
	if p.Nickname == nil || *p.Nickname != "听歌的人" {
		t.Fatal("nickname was not saved")
	}
	for _, input := range []any{map[string]any{}, map[string]any{"nickname": 12}, map[string]any{"nickname": "x\ny"}, map[string]any{"nickname": strings.Repeat("歌", 81)}, map[string]any{"nickname": "ok", "avatar": "fake"}} {
		request("PUT", "/me/profile", input, 400, nil)
	}
	if profile(request("PUT", "/me/profile", map[string]any{"nickname": nil}, 200, nil)).Nickname != nil {
		t.Fatal("nickname did not clear")
	}
	first, second := tracks["admin/first"], tracks["admin/second"]
	favoritePath := "/me/favorites/" + first.ID
	created := request("PUT", favoritePath, nil, 200, nil)
	replayed := request("PUT", favoritePath, nil, 200, nil)
	if created.Body.String() != replayed.Body.String() {
		t.Fatal("favorite replay changed timestamp")
	}
	request("PUT", "/me/favorites/"+second.ID, nil, 200, nil)
	request("PUT", "/me/favorites/"+tracks["other/first"].ID, nil, 404, nil)
	request("PUT", favoritePath, map[string]any{}, 400, nil)
	var page personal.Page
	w := request("GET", "/me/favorites?limit=1", nil, 200, nil)
	_ = json.Unmarshal(w.Body.Bytes(), &page)
	if len(page.Items) != 1 || page.NextCursor == nil {
		t.Fatal("favorites did not paginate")
	}
	request("GET", "/me/favorites?cursor="+*page.NextCursor, nil, 200, nil)
	request("GET", "/me/recently-played?cursor="+*page.NextCursor, nil, 410, nil)
	request("DELETE", favoritePath, nil, 204, nil)
	request("GET", "/me/favorites?cursor="+*page.NextCursor, nil, 410, nil)
	request("DELETE", favoritePath, nil, 204, nil)
	request("DELETE", "/me/favorites", nil, 204, nil)
	for _, query := range []string{"limit=0", "limit=101", "limit=1&limit=2", "limit=01", "cursor=", "unknown=true"} {
		request("GET", "/me/favorites?"+query, nil, 400, nil)
	}
	for _, change := range []func(*http.Request){func(r *http.Request) { r.Header.Del("Cookie") }, func(r *http.Request) { r.Header.Del("X-CSRF-Token") }, func(r *http.Request) { r.Header.Set("Origin", "https://untrusted.invalid") }} {
		status := 403
		r := httptest.NewRequest("PUT", "/", nil)
		r.AddCookie(cookie)
		change(r)
		if r.Header.Get("Cookie") == "" {
			status = 401
		}
		request("PUT", favoritePath, nil, status, change)
	}
	d := playback.Description{SessionID: domain.NewID(), AttemptID: domain.NewID(), AssetID: first.AssetID}
	raw, _ := json.Marshal(d)
	if err := store.PlaybackSave(ctx, d.SessionID, auth.Digest(cookie.Value), "admin", raw, "active", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	input := personal.PlaybackStarted{SourceTrackID: first.ID, PlaybackSessionID: d.SessionID, AttemptID: d.AttemptID}
	one := request("POST", "/me/recently-played", input, 200, nil)
	two := request("POST", "/me/recently-played", input, 200, nil)
	if one.Body.String() != two.Body.String() {
		t.Fatal("playback event replay changed history")
	}
	wrong := input
	wrong.AttemptID = "old-attempt"
	request("POST", "/me/recently-played", wrong, 409, nil)
	wrong = input
	wrong.SourceTrackID = second.ID
	request("POST", "/me/recently-played", wrong, 409, nil)
	wrong = input
	wrong.PlaybackSessionID = "unknown"
	request("POST", "/me/recently-played", wrong, 404, nil)
	request("DELETE", "/me/recently-played/"+first.ID, nil, 204, nil)
	w = request("GET", "/me/recently-played", nil, 200, nil)
	_ = json.Unmarshal(w.Body.Bytes(), &page)
	if len(page.Items) != 0 {
		t.Fatal("history did not clear")
	}
	request("DELETE", "/me/recently-played", nil, 204, nil)
}
