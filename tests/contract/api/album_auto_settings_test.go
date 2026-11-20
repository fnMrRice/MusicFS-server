package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/scraper/online"
	"github.com/fnMrRice/MusicFS-server/src/metadata/onlinescrape"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestAlbumAutoSettingsOrderPersistsPerUser(t *testing.T) {
	server, store, _ := libraryHTTP(t)
	cookie, session := login(t, server)
	catalog, err := onlinescrape.NewWithSettings(online.Catalog(nil, ""), store)
	if err != nil {
		t.Fatal(err)
	}
	server.RegisterOnlineScrapers(catalog, store, nil, http.DefaultClient)
	call := func(method, body, key string, status int) onlinescrape.AlbumAutoSettings {
		t.Helper()
		r := httptest.NewRequest(method, "/api/v1/scrapers/album-auto-settings", strings.NewReader(body))
		r.AddCookie(cookie)
		r.Header.Set("Origin", origin)
		r.Header.Set("X-CSRF-Token", session.CSRFToken)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Idempotency-Key", key)
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s %s returned %d: %s", method, body, w.Code, w.Body.String())
		}
		var page onlinescrape.AlbumAutoSettings
		if status == 200 {
			testkit.ValidateHTTP(t, "AlbumAutoSettings", w.Body.Bytes())
			if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
				t.Fatal(err)
			}
		}
		return page
	}
	defaults := call("GET", "", "", 200)
	if defaults.Revision != 0 || !slices.Equal(defaults.LocalPluginOrder, []string{"musicfs.scraper.musicbrainz", "musicfs.scraper.itunes", "musicfs.scraper.netease"}) {
		t.Fatal("unexpected default order", defaults)
	}
	for _, list := range []string{`[]`, `["unknown"]`, `["musicfs.scraper.lrclib"]`, `["musicfs.scraper.itunes","musicfs.scraper.itunes"]`} {
		call("PUT", `{"expectedRevision":0,"localPluginOrder":`+list+`}`, "invalid-order", 400)
	}
	const body = `{"expectedRevision":0,"localPluginOrder":["musicfs.scraper.qqmusic","musicfs.scraper.itunes"]}`
	testkit.ValidateHTTP(t, "AlbumAutoSettingsInput", []byte(body))
	call("PUT", body, "save-album-order", 200)
	call("PUT", body, "save-album-order", 200)
	call("PUT", body, "stale-order", 409)
	current := call("GET", "", "", 200)
	if current.Revision != 1 || !slices.Equal(current.LocalPluginOrder, []string{"musicfs.scraper.qqmusic", "musicfs.scraper.itunes"}) {
		t.Fatal("saved order changed", current)
	}
	other, err := catalog.AlbumAutoSettings(t.Context(), "other")
	if err != nil || other.Revision != 0 || !slices.Equal(other.LocalPluginOrder, defaults.LocalPluginOrder) {
		t.Fatal("album scraper settings crossed user boundary", other, err)
	}
	// 插件暂时不可用时仍能调整已有顺序，同时到达的相同请求只产生一个修订。
	offline, err := onlinescrape.NewWithSettings(nil, store)
	if err != nil {
		t.Fatal(err)
	}
	input := onlinescrape.AlbumAutoSettingsInput{ExpectedRevision: &current.Revision,
		LocalPluginOrder: []string{"musicfs.scraper.itunes", "musicfs.scraper.qqmusic"}}
	start, done := make(chan struct{}), make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			_, err := offline.SaveAlbumAutoSettings(t.Context(), "admin", input, "reorder-offline")
			done <- err
		}()
	}
	close(start)
	for range 2 {
		if err := <-done; err != nil {
			t.Fatal("concurrent retry failed", err)
		}
	}
	final := call("GET", "", "", 200)
	if final.Revision != 2 || !slices.Equal(final.LocalPluginOrder, input.LocalPluginOrder) {
		t.Fatal("concurrent retry changed the saved revision or plugin order", final)
	}
}
