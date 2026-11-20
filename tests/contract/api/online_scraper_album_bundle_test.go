package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
	"github.com/fnMrRice/MusicFS-server/src/library/music"
	"github.com/fnMrRice/MusicFS-server/src/metadata/onlinescrape"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestOnlineScraperAlbumBundleRequiresCompleteTracksAndReplays(t *testing.T) {
	server, store, _ := libraryHTTP(t)
	cookie, session := login(t, server)
	album, err := store.SaveMusicAlbum(t.Context(), "admin", "", 0, music.AlbumFields{Name: "Original album"}, "create-bundle-api")
	if err != nil {
		t.Fatal(err)
	}
	service, _ := onlinescrape.New([]scraper.Plugin{albumScraperFixture{}})
	server.RegisterMusicLibrary(store)
	server.RegisterOnlineScrapers(service, store, nil, http.DefaultClient)
	server.RegisterOnlineAlbumScrapers(service, store)
	call := func(method, path string, input any, key string, status int) []byte {
		t.Helper()
		raw, _ := json.Marshal(input)
		r := httptest.NewRequest(method, "/api/v1"+path, strings.NewReader(string(raw)))
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
	search := func(field string) string {
		raw := call("POST", "/scrapers/album-fixture/search", map[string]any{"field": field, "query": map[string]string{"album": "Album"}}, "", 200)
		var page struct{ Items []onlinescrape.Candidate }
		if err := json.Unmarshal(raw, &page); err != nil || len(page.Items) != 1 {
			t.Fatalf("search: %s %v", raw, err)
		}
		return page.Items[0].Token
	}
	path := "/library/albums/" + album.ID + "/scraped-fields"
	details, tracks := search("albumDetails"), search("albumTracks")
	input := map[string]any{"expectedRevision": 1, "pluginId": "album-fixture", "candidates": map[string]string{"albumDetails": details}}
	call("PUT", path, input, "incomplete-bundle", 400)
	input["candidates"] = map[string]string{"albumDetails": details, "albumTracks": details}
	call("PUT", path, input, "wrong-field-bundle", 404)
	input["candidates"] = map[string]string{"albumDetails": details, "albumTracks": tracks}
	raw, _ := json.Marshal(input)
	testkit.ValidateHTTP(t, "MusicAlbumScrapedBundleInput", raw)
	first := call("PUT", path, input, "select-whole-bundle", 200)
	testkit.ValidateHTTP(t, "ScrapedMusicAlbumRevision", first)
	call("PUT", path, input, "select-whole-bundle", 200)
	call("PUT", path, input, "stale-whole-bundle", 409)
	page, err := store.MusicTracks(t.Context(), "admin", music.Query{Limit: 10, AlbumID: album.ID})
	if err != nil || len(page.Items) != 1 || page.Items[0].Title != "Encore" || page.Items[0].SelectedSource != nil {
		t.Fatalf("bundle did not create missing track: %+v %v", page, err)
	}
	saved, err := store.MusicAlbumScrapedFields(t.Context(), "admin", album.ID)
	if err != nil || len(saved) != 2 {
		t.Fatalf("bundle fields: %+v %v", saved, err)
	}
	call("PUT", "/library/albums/"+album.ID, map[string]any{"name": "Manual alias", "expectedRevision": 2, "sourceLinks": []music.AlbumSourceLink{{URL: "https://example.com/manual-page", PluginID: "album-fixture"}}}, "save-page-without-refresh", 200)
	mergePath := "/library/albums/" + album.ID + "/metadata-merge"
	mergeInput := map[string]any{"expectedRevision": 2, "pluginId": "album-fixture", "candidates": map[string]string{"albumDetails": details, "albumTracks": tracks}, "fields": []string{"name", "publisher"}}
	call("PUT", mergePath, mergeInput, "reject-old-comparison", 409)
	mergeInput["expectedRevision"] = 3
	raw, _ = json.Marshal(mergeInput)
	testkit.ValidateHTTP(t, "MusicAlbumMergeInput", raw)
	merged := call("PUT", mergePath, mergeInput, "accept-selected-comparison", 200)
	current, err := store.MusicAlbum(t.Context(), "admin", album.ID)
	if err != nil || current.Name != "Online album" || current.OriginalName != "Original album" || len(current.SourceLinks) != 1 || current.SourceLinks[0].URL != "https://example.com/manual-page" {
		t.Fatalf("merged album/source link: %+v %v", current, err)
	}
	mergeInput["expectedRevision"] = 4
	mergeInput["fields"] = []string{"unknown"}
	call("PUT", mergePath, mergeInput, "reject-invalid-merge-field", 400)
	mergeInput["expectedRevision"] = 3
	mergeInput["fields"] = []string{"name", "publisher"}
	fresh, _ := onlinescrape.New(nil)
	server, _ = testServer()
	cookie, session = login(t, server)
	server.RegisterOnlineAlbumScrapers(fresh, store)
	if replay := call("PUT", path, input, "select-whole-bundle", 200); string(replay) != string(first) {
		t.Fatal("replay changed after candidate expiration")
	}
	if replay := call("PUT", mergePath, mergeInput, "accept-selected-comparison", 200); string(replay) != string(merged) {
		t.Fatal("merge replay changed after candidate expiration")
	}
	mergeInput["expectedRevision"] = 4
	call("PUT", mergePath, mergeInput, "expired-new-comparison", 404)
	input["expectedRevision"] = 4
	call("PUT", path, input, "expired-new-bundle", 404)
}
