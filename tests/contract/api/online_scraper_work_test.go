package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
	"github.com/fnMrRice/MusicFS-server/src/library/works"
	"github.com/fnMrRice/MusicFS-server/src/metadata/onlinescrape"
)

type discographyFixture struct{}

func (discographyFixture) Descriptor() scraper.Descriptor {
	return scraper.Descriptor{PluginID: "fixture", Name: "Discography", Type: "scraper", Fields: []string{"albumTracks"}, Available: true}
}
func (discographyFixture) Search(_ context.Context, _ string, q scraper.Query) ([]scraper.Item, error) {
	return []scraper.Item{{ID: "release-1", Title: q.Album, Tracks: []scraper.Track{{Disc: 2, Number: 1, Title: "Encore"}}}}, nil
}

func TestOnlineScraperWorkSelection(t *testing.T) {
	server, store, _ := libraryHTTP(t)
	cookie, session := login(t, server)
	ctx := t.Context()
	work, err := store.CreateWork(ctx, "admin", works.CreateRequest{ContentType: "cd_album", Title: "My handwritten title"}, "create-work-one")
	if err != nil {
		t.Fatal(err)
	}
	service, err := onlinescrape.New([]scraper.Plugin{discographyFixture{}})
	if err != nil {
		t.Fatal(err)
	}
	server.RegisterOnlineScrapers(service, store, nil, http.DefaultClient)
	call := func(method, path, body, key string, status int) []byte {
		t.Helper()
		r := httptest.NewRequest(method, "/api/v1"+path, strings.NewReader(body))
		r.AddCookie(cookie)
		r.Header.Set("Origin", origin)
		r.Header.Set("X-CSRF-Token", session.CSRFToken)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Idempotency-Key", key)
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
		}
		return w.Body.Bytes()
	}
	body := call("GET", "/scrapers", "", "", 200)
	if !strings.Contains(string(body), `"type":"scraper"`) {
		t.Fatal("missing plugin type")
	}
	path := "/scrapers/fixture/search"
	call("POST", path, `{"field":"artistImage","query":{"artist":"A"}}`, "", 415)
	reply := call("POST", path, `{"field":"albumTracks","query":{"album":"Live"}}`, "", 200)
	var page struct{ Items []onlinescrape.Candidate }
	if err := json.Unmarshal(reply, &page); err != nil || len(page.Items) != 1 || page.Items[0].Token == "" {
		t.Fatalf("invalid candidates: %s", reply)
	}
	token := page.Items[0].Token
	if _, err = service.Candidate("someone-else", "fixture", "albumTracks", token); err == nil {
		t.Fatal("candidate leaked across users")
	}
	fields := "/works/" + work.ID + "/scraped-fields"
	if body := call("GET", fields, "", "", 200); !strings.Contains(string(body), `"items":[]`) {
		t.Fatal("search modified the work")
	}
	selection := `{"expectedRevision":1,"pluginId":"fixture","candidateToken":"` + token + `"}`
	call("PUT", fields+"/artistImage", selection, "apply-invalid", 404)
	first := call("PUT", fields+"/albumTracks", selection, "apply-once", 200)
	call("PUT", fields+"/albumTracks", selection, "apply-once", 200)
	call("PUT", fields+"/albumTracks", selection, "apply-stale", 409)
	result, err := store.Work(ctx, "admin", work.ID)
	if err != nil || result.Title != work.Title || result.Revision != 2 {
		t.Fatalf("work changed unexpectedly: %+v %v", result, err)
	}
	_, err = store.UpdateWork(ctx, "admin", work.ID, works.UpdateRequest{ExpectedRevision: 2, Title: "Edited"}, "edit-after-scrape")
	if err != nil {
		t.Fatal(err)
	}
	reply = call("GET", fields, "", "", 200)
	if !strings.Contains(string(reply), `"title":"Encore"`) || !strings.Contains(string(reply), `"pluginId":"fixture"`) {
		t.Fatalf("edit erased source data: %s", reply)
	}
	if _, err = store.WorkScrapedFields(ctx, "someone-else", work.ID); err == nil {
		t.Fatal("saved fields leaked across users")
	}
	if strings.Contains(string(reply), token) {
		t.Fatal("saved field includes the short-lived token")
	}
	fresh, err := onlinescrape.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	server, _ = testServer()
	cookie, session = login(t, server)
	server.RegisterOnlineScrapers(fresh, store, nil, http.DefaultClient)
	replay := call("PUT", fields+"/albumTracks", selection, "apply-once", 200)
	if string(replay) != string(first) {
		t.Fatalf("different response on replay: %s, %s", first, replay)
	}
	call("PUT", fields+"/albumTracks", strings.ReplaceAll(selection, token, "different-token"), "apply-once", 409)
	call("PUT", fields+"/albumTracks", selection, "expired-attempt", 404)
}
