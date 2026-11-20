package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/scraper/online"
	"github.com/fnMrRice/MusicFS-server/sdk/fault"
	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
	"github.com/fnMrRice/MusicFS-server/src/metadata/onlinescrape"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestOnlineScraperHTTPReportsPlatformStatus(t *testing.T) {
	for _, tc := range []struct {
		code, hint string
		status     int
	}{
		{"RATE_LIMITED", "", 429},
		{"SOURCE_UNAVAILABLE", "0", 503},
	} {
		t.Run(tc.code, func(t *testing.T) {
			server, _ := testServer()
			cookie, session := login(t, server)
			attempts := 0
			client := &http.Client{Transport: lyricsTransport(func(req *http.Request) (*http.Response, error) {
				attempts++
				header := make(http.Header)
				if tc.hint != "" {
					header.Set("Retry-After", tc.hint)
				}
				return &http.Response{StatusCode: tc.status, Header: header, Body: io.NopCloser(strings.NewReader("private upstream details"))}, nil
			})}
			catalog, err := onlinescrape.New([]scraper.Plugin{online.Bangumi(client)})
			if err != nil {
				t.Fatal(err)
			}
			server.RegisterOnlineScrapers(catalog, nil, nil, client)
			req := httptest.NewRequest("POST", "/api/v1/scrapers/musicfs.scraper.bangumi/search", strings.NewReader(`{"field":"albumDetails","query":{"album":"Music"}}`))
			req.AddCookie(cookie)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-CSRF-Token", session.CSRFToken)
			req.Header.Set("Origin", origin)
			w := httptest.NewRecorder()
			server.ServeHTTP(w, req)
			var result struct {
				Error fault.Error `json:"error"`
			}
			if w.Code != tc.status || w.Header().Get("Retry-After") != tc.hint || attempts != 1 || strings.Contains(w.Body.String(), "private upstream details") {
				t.Fatalf("unexpected upstream failure: status=%d attempts=%d", w.Code, attempts)
			}
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			info := result.Error.Scraper
			if result.Error.Code != tc.code || result.Error.RequestID == "" || (result.Error.RetryAfter != nil) != (tc.hint != "") || info == nil || info.PluginID != "musicfs.scraper.bangumi" || info.Reason != "upstream" || info.UpstreamStatus != tc.status {
				t.Fatalf("missing platform error details: %s", w.Body)
			}
			testkit.ValidateHTTP(t, "Error", w.Body.Bytes())
		})
	}
}

type busySearchFixture struct {
	albumScraperFixture
	entered, release chan struct{}
}

func (p busySearchFixture) Search(context.Context, string, scraper.Query) ([]scraper.Item, error) {
	p.entered <- struct{}{}
	<-p.release
	return nil, nil
}

func TestOnlineScraperReportsLocalSearchCapacity(t *testing.T) {
	plugin := busySearchFixture{entered: make(chan struct{}, 8), release: make(chan struct{})}
	service, err := onlinescrape.New([]scraper.Plugin{plugin})
	if err != nil {
		t.Fatal(err)
	}
	search := func() error {
		_, err := service.Search(t.Context(), "user", "album-fixture", "albumDetails", scraper.Query{Album: "Album"})
		return err
	}
	var pending sync.WaitGroup
	defer pending.Wait()
	defer close(plugin.release)
	for range cap(plugin.entered) {
		pending.Go(func() { _ = search() })
		<-plugin.entered
	}
	var problem *fault.Error
	if err := search(); !errors.As(err, &problem) || problem.Code != "RATE_LIMITED" || problem.RetryAfter != nil || problem.Scraper == nil ||
		*problem.Scraper != (fault.ScraperFailure{Reason: "busy", PluginID: "album-fixture"}) {
		t.Fatalf("unexpected local capacity error: %v", err)
	}
}
