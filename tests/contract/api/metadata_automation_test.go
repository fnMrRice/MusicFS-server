package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/transform/thumbnail"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/library/jobs"
	"github.com/fnMrRice/MusicFS-server/src/metadata/enrichment"
	"github.com/fnMrRice/MusicFS-server/src/metadata/enrichmentworker"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestMetadataAutomationHTTPBoundedPagesAndOwnerIsolation(t *testing.T) {
	server, store, cookie := libraryHTTP(t)
	ctx := context.Background()
	scans := map[string]string{}
	for _, owner := range []string{"admin", "other"} {
		plan := testkit.ScanPlan(owner)
		plan.Options.ImportLyrics = true
		if _, _, err := store.Enqueue(ctx, owner, fixtureInstanceID(owner), plan, "automation-http-seed"); err != nil {
			t.Fatal(err)
		}
		scan, err := store.Claim(ctx)
		if err != nil {
			t.Fatal(err)
		}
		items := []library.Observation{}
		for i := range 2 {
			name := fmt.Sprintf("%d.wav", i)
			items = append(items, library.Observation{Observation: pluginproto.Observation{ExternalID: name, RootID: owner, RelativePath: "private-source-path/" + name, DisplayName: name, ObservedVersion: "version", SizeBytes: 1}, Artists: []string{}})
		}
		if err := store.Publish(ctx, scan, testkit.ScanPublication(scan, items)); err != nil {
			t.Fatal(err)
		}
		if err := store.Finish(ctx, scan, 2, nil); err != nil {
			t.Fatal(err)
		}
		scans[owner] = scan.ID
	}
	files, err := mediastore.OpenFiles(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { files.Close() })
	service, err := enrichmentworker.New(store, library.New(store, jobs.New(store), nil, nil), mediastore.NewContent(store, store, files, thumbnail.New()), nil, enrichmentworker.Config{})
	if err != nil {
		t.Fatal(err)
	}
	server.RegisterMetadataAutomation(service)
	request := func(scan, suffix string, authenticated bool) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest("GET", "/api/v1/scan-jobs/"+scan+"/metadata-results"+suffix, nil)
		if authenticated {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		if bytes.Contains(w.Body.Bytes(), []byte("private-source-path")) || bytes.Contains(w.Body.Bytes(), []byte(`"policy"`)) || bytes.Contains(w.Body.Bytes(), []byte(`"expectedRevision"`)) || bytes.Contains(w.Body.Bytes(), []byte(`"owner"`)) {
			t.Fatal("automation result disclosed internal state")
		}
		return w
	}
	seen := map[string]bool{}
	cursor := ""
	for i := range 4 {
		w := request(scans["admin"], "?limit=1&cursor="+cursor, true)
		if w.Code != 200 {
			t.Fatalf("page: %d %s", w.Code, w.Body.String())
		}
		testkit.ValidateHTTP(t, "MetadataAutomationPage", w.Body.Bytes())
		var page enrichment.AutomationPage
		if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil || len(page.Items) != 1 {
			t.Fatal("wrong page size", err)
		}
		item := page.Items[0]
		if seen[item.TriggerID] || item.ScanJobID != scans["admin"] || item.State != "queued" || len(item.Actions) != 0 {
			t.Fatalf("invalid page result: %+v", item)
		}
		seen[item.TriggerID] = true
		if i == 3 {
			if page.NextCursor != nil {
				t.Fatal("last page supplied a cursor")
			}
		} else if page.NextCursor == nil {
			t.Fatal("missing next cursor")
		} else {
			cursor = *page.NextCursor
		}
	}
	private, err := store.ScanAutomationResults(ctx, "other", scans["other"], "", 1)
	if err != nil || len(private.Items) != 1 {
		t.Fatal("private fixture", err)
	}
	for _, tc := range []struct {
		scan, suffix string
		status       int
		auth         bool
	}{
		{scans["admin"], "", 401, false},
		{scans["other"], "", 404, true},
		{"absent-scan", "", 404, true},
		{scans["admin"], "?cursor=" + private.Items[0].TriggerID, 410, true},
		{scans["admin"], "?cursor=absent", 410, true},
		{scans["admin"], "?limit=0", 400, true},
		{scans["admin"], "?limit=201", 400, true},
		{scans["admin"], "?limit=", 400, true},
		{scans["admin"], "?limit=1.5", 400, true},
		{scans["admin"], "?limit=1&limit=2", 400, true},
		{scans["admin"], "?cursor=a&cursor=b", 400, true},
		{scans["admin"], "?cursor=" + strings.Repeat("x", 257), 400, true},
		{scans["admin"], "?owner=other", 400, true},
		{scans["admin"], "?limit=1;owner=other", 400, true},
		{scans["admin"], "?cursor=%GG", 400, true},
	} {
		w := request(tc.scan, tc.suffix, tc.auth)
		if w.Code != tc.status {
			t.Fatalf("%s: %d %s", tc.suffix, w.Code, w.Body.String())
		}
		testkit.ValidateHTTP(t, "Error", w.Body.Bytes())
	}
}
