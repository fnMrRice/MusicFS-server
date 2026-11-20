package api_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/scraper/online"
	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
	"github.com/fnMrRice/MusicFS-server/src/metadata/onlinescrape"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestOnlineScraperHTTPAlbumPreviewCacheAndRefresh(t *testing.T) {
	for _, field := range []string{"albumDetails", "albumTracks"} {
		t.Run(field, func(t *testing.T) {
			server, _ := testServer()
			cookie, session := login(t, server)
			calls, status, title := 0, http.StatusOK, "First edition"
			client := &http.Client{Transport: lyricsTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.Host != "music.163.com" || r.URL.Path != "/api/v1/album/23" {
					return nil, fmt.Errorf("unexpected upstream request: %s", r.URL)
				}
				body := fmt.Sprintf(`{"code":200,"album":{"id":23,"name":%q,"size":1},"songs":[{"id":77,"name":%q}]}`, title, title)
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
			})}
			catalog, err := onlinescrape.New([]scraper.Plugin{online.NetEase(client)})
			if err != nil {
				t.Fatal(err)
			}
			server.RegisterOnlineScrapers(catalog, nil, nil, client)
			search := func(field string, refresh bool, wantStatus int) onlinescrape.Candidate {
				t.Helper()
				query := scraper.Query{ExternalID: "23"}
				if field == "albumDetails" || field == "albumTracks" {
					query.SourceURL = "https://music.163.com/album?id=23"
				}
				input := map[string]any{"field": field, "query": query}
				if refresh {
					input["refresh"] = true
				}
				raw, err := json.Marshal(input)
				if err != nil {
					t.Fatal(err)
				}
				r := httptest.NewRequest("POST", "/api/v1/scrapers/musicfs.scraper.netease/search", bytes.NewReader(raw))
				r.AddCookie(cookie)
				r.Header.Set("Content-Type", "application/json")
				r.Header.Set("X-CSRF-Token", session.CSRFToken)
				r.Header.Set("Origin", origin)
				w := httptest.NewRecorder()
				server.ServeHTTP(w, r)
				if w.Code != wantStatus {
					t.Fatalf("%s search refresh=%t returned %d: %s", field, refresh, w.Code, w.Body)
				}
				if wantStatus != http.StatusOK {
					testkit.ValidateHTTP(t, "Error", w.Body.Bytes())
					return onlinescrape.Candidate{}
				}
				testkit.ValidateHTTP(t, "OnlineScraperCandidates", w.Body.Bytes())
				var page struct{ Items []onlinescrape.Candidate }
				if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil || len(page.Items) != 1 {
					t.Fatalf("unexpected album search response: %s", w.Body)
				}
				item := page.Items[0]
				if item.ID != "23" || item.Token == "" || field == "albumTracks" && (len(item.Tracks) != 1 || item.Tracks[0].Title != item.Title) {
					t.Fatalf("incomplete candidate: %+v", item)
				}
				return item
			}

			first := search(field, false, http.StatusOK)
			title = "Revised edition"
			reopened := search(field, false, http.StatusOK)
			if first.Title != "First edition" || reopened.Title != first.Title || calls != 1 {
				t.Fatalf("reopening changed the preview or repeated the upstream call: %+v, calls=%d", reopened, calls)
			}
			updated := search(field, true, http.StatusOK)
			if updated.Title != title || updated.Token == first.Token || calls != 2 {
				t.Fatalf("refresh did not read and issue a new candidate: %+v, calls=%d", updated, calls)
			}
			status = http.StatusServiceUnavailable
			search(field, true, http.StatusServiceUnavailable)
			if cached := search(field, false, http.StatusOK); cached.Title != updated.Title || calls != 3 {
				t.Fatalf("failed refresh lost the last successful preview: %+v, calls=%d", cached, calls)
			}
			search("lyrics", true, http.StatusBadRequest)
			if calls != 3 {
				t.Fatal("non-album refresh reached the platform")
			}
		})
	}
}
