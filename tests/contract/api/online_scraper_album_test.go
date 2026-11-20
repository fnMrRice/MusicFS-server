package api_test

import (
	"context"
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

type albumScraperFixture struct{}

func (albumScraperFixture) Descriptor() scraper.Descriptor {
	return scraper.Descriptor{PluginID: "album-fixture", Name: "Album fixture", Type: "scraper", Available: true,
		Fields: []string{"albumDetails", "albumTracks", "artwork", "lyrics"}, ImageHosts: []string{"example.com"}}
}

func (albumScraperFixture) Search(_ context.Context, field string, _ scraper.Query) ([]scraper.Item, error) {
	item := scraper.Item{ID: "release-1", Title: "Online album", SourceURL: "https://example.com/release-1"}
	switch field {
	case "albumDetails":
		item.Description, item.Date = "Online description", "2026-11-20"
	case "albumTracks":
		item.Tracks = []scraper.Track{{Title: "Encore", Disc: 2, Number: 1}}
	case "artwork":
		item.ImageURL = "https://example.com/cover.jpg"
	case "lyrics":
		item.Lyrics = "Words"
	}
	return []scraper.Item{item}, nil
}

func TestOnlineScraperMusicAlbumSelection(t *testing.T) {
	server, store, _ := libraryHTTP(t)
	cookie, session := login(t, server)
	ctx := t.Context()
	album, err := store.SaveMusicAlbum(ctx, "admin", "", 0, music.AlbumFields{Name: "Handwritten album"}, "create-scrape-album")
	if err != nil {
		t.Fatal(err)
	}
	other, err := store.SaveMusicAlbum(ctx, "other", "", 0, music.AlbumFields{Name: "Private album"}, "create-scrape-other")
	if err != nil {
		t.Fatal(err)
	}
	track, err := store.SaveMusicTrack(ctx, "admin", "", 0, music.TrackInput{TrackFields: music.TrackFields{Title: "Original track"}, AlbumIDs: []string{album.ID}}, "create-scrape-track")
	if err != nil {
		t.Fatal(err)
	}
	service, err := onlinescrape.New([]scraper.Plugin{albumScraperFixture{}})
	if err != nil {
		t.Fatal(err)
	}
	server.RegisterMusicLibrary(store)
	server.RegisterOnlineScrapers(service, store, nil, http.DefaultClient)
	server.RegisterOnlineAlbumScrapers(service, store)
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
	fields := "/library/albums/" + album.ID + "/scraped-fields"
	unauthorized := httptest.NewRecorder()
	server.ServeHTTP(unauthorized, httptest.NewRequest("GET", "/api/v1"+fields, nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("album metadata did not require authentication: %d", unauthorized.Code)
	}
	before := call("GET", "/library/tracks", "", "", 200)
	call("GET", "/library/albums/"+other.ID+"/scraped-fields", "", "", 404)
	if body := call("GET", fields, "", "", 200); !strings.Contains(string(body), `"items":[]`) {
		t.Fatalf("unexpected initial metadata: %s", body)
	}
	search := func(field string) string {
		t.Helper()
		body := call("POST", "/scrapers/album-fixture/search", `{"field":"`+field+`","query":{"album":"Original"}}`, "", 200)
		var page struct{ Items []onlinescrape.Candidate }
		if err := json.Unmarshal(body, &page); err != nil || len(page.Items) != 1 {
			t.Fatalf("invalid search result: %s", body)
		}
		return page.Items[0].Token
	}
	selection := func(revision int64, token string) string {
		raw, err := json.Marshal(map[string]any{"expectedRevision": revision, "pluginId": "album-fixture", "candidateToken": token})
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	token := search("albumDetails")
	call("GET", fields, "", "", 200)
	call("PUT", fields+"/albumDetails", selection(1, token), "", 400)
	call("PUT", fields+"/lyrics", selection(1, search("lyrics")), "reject-lyrics", 400)
	call("PUT", fields+"/albumDetails", `{"pluginId":"album-fixture","candidateToken":"`+token+`"}`, "missing-revision", 400)
	call("PUT", "/library/albums/"+other.ID+"/scraped-fields/albumDetails", selection(1, token), "reject-other-album", 404)
	foreign, err := service.Search(ctx, "other", "album-fixture", "albumDetails", scraper.Query{Album: "Private"})
	if err != nil || len(foreign) != 1 {
		t.Fatal(err)
	}
	call("PUT", fields+"/albumDetails", selection(1, foreign[0].Token), "reject-foreign-token", 404)
	first := call("PUT", fields+"/albumDetails", selection(1, token), "select-album-details", 200)
	testkit.ValidateHTTP(t, "ScrapedMusicAlbumRevision", first)
	call("PUT", fields+"/albumDetails", selection(1, token), "select-album-details", 200)
	call("PUT", fields+"/albumDetails", selection(1, token), "stale-album-details", 409)
	call("PUT", fields+"/albumTracks", selection(2, search("albumTracks")), "select-album-tracks", 200)
	call("PUT", fields+"/artwork", selection(3, search("artwork")), "select-album-cover", 200)
	result, err := store.MusicAlbum(ctx, "admin", album.ID)
	if err != nil || result.Revision != 4 || result.Name != album.Name || result.CoverPath == nil || *result.CoverPath != "https://example.com/cover.jpg" {
		t.Fatalf("unexpected album fields: %+v %v", result, err)
	}
	var storedCover *string
	if err = store.DB.QueryRowContext(ctx, "SELECT cover_path FROM music_albums WHERE id=?", album.ID).Scan(&storedCover); err != nil || storedCover != nil {
		t.Fatalf("computed album cover overwrote the stored main field: %v %v", storedCover, err)
	}
	var oldPage, newPage music.TrackPage
	if err = json.Unmarshal(before, &oldPage); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(call("GET", "/library/tracks", "", "", 200), &newPage); err != nil || newPage.LibraryRevision == oldPage.LibraryRevision {
		t.Fatal("scraping did not advance library revision", err)
	}
	if _, err = store.SaveMusicAlbum(ctx, "admin", album.ID, 4, music.AlbumFields{Name: "Edited name"}, "edit-album-after-scrape"); err != nil {
		t.Fatal(err)
	}
	saved := call("GET", fields, "", "", 200)
	testkit.ValidateHTTP(t, "MusicAlbumScrapedFields", saved)
	if !strings.Contains(string(saved), `"title":"Encore"`) || !strings.Contains(string(saved), `"imageUrl":"https://example.com/cover.jpg"`) || strings.Contains(string(saved), token) {
		t.Fatalf("saved metadata was lost or exposed tokens: %s", saved)
	}
	current, err := store.MusicTrack(ctx, "admin", track.ID)
	if err != nil || current.Title != track.Title || current.Revision != track.Revision || len(current.Albums) != 1 || current.Albums[0].ID != album.ID {
		t.Fatalf("album scraping changed track metadata or membership: %+v %v", current, err)
	}
	fresh, _ := onlinescrape.New(nil)
	server, _ = testServer()
	cookie, session = login(t, server)
	server.RegisterOnlineAlbumScrapers(fresh, store)
	if replay := call("PUT", fields+"/albumDetails", selection(1, token), "select-album-details", 200); string(replay) != string(first) {
		t.Fatalf("replay after restart differs: %s, %s", replay, first)
	}
	call("PUT", fields+"/albumDetails", selection(1, "another-token"), "select-album-details", 409)
	call("PUT", fields+"/albumDetails", selection(5, token), "expired-new-selection", 404)
}
