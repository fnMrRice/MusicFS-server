package api_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
	"github.com/fnMrRice/MusicFS-server/src/library/music"
	"github.com/fnMrRice/MusicFS-server/src/library/works"
	"github.com/fnMrRice/MusicFS-server/src/metadata/onlinescrape"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestClearAlbumScrapedFieldsHTTP(t *testing.T) {
	server, store, _ := libraryHTTP(t)
	cookie, session := login(t, server)
	service, err := onlinescrape.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	server.RegisterOnlineAlbumScrapers(service, store)
	ctx := t.Context()
	album := func(owner string) string {
		t.Helper()
		created, err := store.SaveMusicAlbum(ctx, owner, "", 0, music.AlbumFields{Name: "Saved album"}, "create-private-album")
		if err != nil {
			t.Fatal(err)
		}
		return created.ID
	}
	id, other := album("admin"), album("other")
	selectDetails := func(id, owner string, revision int64, key string) {
		t.Helper()
		field := works.ScrapedField{Field: "albumDetails", PluginID: "album-fixture", CandidateToken: "details-token",
			Item: scraper.Item{ID: "release", Title: "Platform album"}}
		if _, err := store.SelectMusicAlbumScrapedField(ctx, owner, id, revision, field, key); err != nil {
			t.Fatal(err)
		}
	}
	selectDetails(id, "admin", 1, "add-details")
	selectDetails(other, "other", 1, "add-details")
	clear := func(id, body, key string, auth bool, csrf string, want int) []byte {
		t.Helper()
		r := httptest.NewRequest(http.MethodDelete, "/api/v1/library/albums/"+id+"/scraped-fields", strings.NewReader(body))
		if auth {
			r.AddCookie(cookie)
		}
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", origin)
		r.Header.Set("X-CSRF-Token", csrf)
		r.Header.Set("Idempotency-Key", key)
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("clear %s, expected %d, received %d: %s", key, want, w.Code, w.Body.String())
		}
		return w.Body.Bytes()
	}
	token := session.CSRFToken
	const body = `{"expectedRevision":2}`
	testkit.ValidateHTTP(t, "ClearMusicAlbumScrapedInput", []byte(body))
	clear(id, body, "without-session", false, token, 401)
	clear(id, body, "without-csrf", true, "", 403)
	clear(id, body, "", true, token, 400)
	clear(id, "{}", "missing-revision", true, token, 400)
	clear(other, body, "foreign-album", true, token, 404)
	clear(id, `{"expectedRevision":1}`, "stale-clear", true, token, 409)
	first := clear(id, body, "clear-album", true, token, 200)
	testkit.ValidateHTTP(t, "ScrapedMusicAlbumRevision", first)
	if !strings.Contains(string(first), `"revision":3`) {
		t.Fatalf("unexpected clear revision: %s", first)
	}
	empty := clear(id, `{"expectedRevision":3}`, "already-empty", true, token, 200)
	if string(empty) != string(first) {
		t.Fatalf("empty clear advanced the revision: %s", empty)
	}
	checkFields := func(owner, id string, count int) {
		t.Helper()
		saved, err := store.MusicAlbumScrapedFields(ctx, owner, id)
		if err != nil || len(saved) != count {
			t.Fatalf("album %s has %d fields, expected %d: %v", id, len(saved), count, err)
		}
	}
	checkFields("admin", id, 0)
	checkFields("other", other, 1)
	selectDetails(id, "admin", 3, "add-new-details")
	if replay := clear(id, body, "clear-album", true, token, 200); string(replay) != string(first) {
		t.Fatalf("clear replay changed: %s", replay)
	}
	checkFields("admin", id, 1)
}
