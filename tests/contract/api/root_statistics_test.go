package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestRootStatisticsHTTPIsReadOnlyPrivateAndAvailableOffline(t *testing.T) {
	const provider = "316664dc-7106-5acd-8df7-88c9d0d9baef"
	server, store, cookie := libraryHTTP(t)
	ctx := context.Background()
	read := func() library.RootStatistics {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, "/api/v1/provider-instances/"+provider+"/root-statistics", nil)
		r.AddCookie(cookie)
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("root statistics HTTP %d: %s", w.Code, w.Body.String())
		}
		testkit.ValidateHTTP(t, "RootStatistics", w.Body.Bytes())
		for _, private := range []string{"private", `"owner"`, `"path"`, `"assetId"`} {
			if strings.Contains(w.Body.String(), private) {
				t.Fatalf("root statistics exposed internal data: %s", private)
			}
		}
		var result library.RootStatistics
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	if result := read(); result.Roots == nil || len(result.Roots) != 0 {
		t.Fatalf("empty statistics = %+v", result)
	}
	if _, _, err := store.Enqueue(ctx, "admin", provider, testkit.ScanPlan("music"), "statistics-fixture"); err != nil {
		t.Fatal(err)
	}
	job, err := store.Claim(ctx)
	if err != nil {
		t.Fatal(err)
	}
	item := library.Observation{Observation: pluginproto.Observation{ExternalID: "one", RootID: "music",
		RelativePath: "private/song.flac", ObservedVersion: "v1", DisplayName: "Private name", MIME: "audio/flac", SizeBytes: 12345}}
	if err := store.Publish(ctx, job, testkit.ScanPublication(job, []library.Observation{item})); err != nil {
		t.Fatal(err)
	}
	if err := store.Finish(ctx, job, 1, nil); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if result := read(); len(result.Roots) != 1 || result.Roots[0] != (library.RootStatistic{RootID: "music", TrackCount: 1, SizeBytes: 12345}) {
			t.Fatalf("published statistics unavailable offline: %+v", result)
		}
	}
	var count, attempts int
	if err := store.DB.QueryRowContext(ctx, "SELECT COUNT(*),SUM(attempt_count) FROM jobs").Scan(&count, &attempts); err != nil || count != 1 || attempts != 1 {
		t.Fatalf("statistics GET changed scans: count=%d attempts=%d error=%v", count, attempts, err)
	}
	for _, test := range []struct {
		provider string
		cookie   *http.Cookie
		status   int
	}{{provider, nil, http.StatusUnauthorized}, {"843558e1-a158-56ac-834a-baffd12bb5c5", cookie, http.StatusNotFound}, {"missing", cookie, http.StatusNotFound}} {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/provider-instances/"+test.provider+"/root-statistics", nil)
		if test.cookie != nil {
			r.AddCookie(test.cookie)
		}
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		if w.Code != test.status {
			t.Fatalf("unauthorized statistics HTTP %d: %s", w.Code, w.Body.String())
		}
		testkit.ValidateHTTP(t, "Error", w.Body.Bytes())
	}
}
