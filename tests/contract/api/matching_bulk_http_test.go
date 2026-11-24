package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/matching/standard"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/metadata/matching"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestMatchingBulkMergeHTTPValidationPrivacyAndIdempotency(t *testing.T) {
	server, store, _ := libraryHTTP(t)
	cookie, session := login(t, server)
	server.RegisterMatching(matching.New(store, nil, standard.Plugin{}))
	ctx := t.Context()
	privateIDs := []string{}
	for _, owner := range []string{"admin", "other"} {
		provider := "316664dc-7106-5acd-8df7-88c9d0d9baef"
		if owner == "other" {
			provider = "843558e1-a158-56ac-834a-baffd12bb5c5"
		}
		if _, _, err := store.Enqueue(ctx, owner, provider, testkit.ScanPlan(provider), "bulk-http-seed"); err != nil {
			t.Fatal(err)
		}
		job, err := store.Claim(ctx)
		if err != nil {
			t.Fatal(err)
		}
		title := "Same title in both accounts"
		items := []library.Observation{}
		for _, name := range []string{"one", "two"} {
			items = append(items, library.Observation{Observation: pluginproto.Observation{ExternalID: name, RootID: provider, RelativePath: "secret-path/" + name + ".wav", DisplayName: name, ObservedVersion: "v1", SizeBytes: 100}, Title: &title, Artists: []string{"Same artist"}})
		}
		if err := store.Publish(ctx, job, testkit.ScanPublication(job, items)); err != nil {
			t.Fatal(err)
		}
		if err := store.Finish(ctx, job, len(items), nil); err != nil {
			t.Fatal(err)
		}
		if owner == "other" {
			page, err := store.ListTracks(ctx, owner, provider, "", 10)
			if err != nil {
				t.Fatal(err)
			}
			for _, track := range page.Items {
				privateIDs = append(privateIDs, track.ID)
			}
		}
	}
	request := func(method, path, body, key string, mutate func(*http.Request)) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, "/api/v1"+path, strings.NewReader(body))
		r.AddCookie(cookie)
		r.Header.Set("Origin", origin)
		r.Header.Set("X-CSRF-Token", session.CSRFToken)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Idempotency-Key", key)
		if mutate != nil {
			mutate(r)
		}
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		for _, private := range append(privateIDs, "secret-path", `"owner"`) {
			if strings.Contains(w.Body.String(), private) {
				t.Fatal("bulk result leaked another owner or internal field")
			}
		}
		if w.Code >= 400 {
			testkit.ValidateHTTP(t, "Error", w.Body.Bytes())
		}
		return w
	}
	checkRevision := func(want int64) {
		t.Helper()
		revision := request("GET", "/matching/revision", "", "", nil)
		testkit.ValidateHTTP(t, "MatchingRevision", revision.Body.Bytes())
		var result struct {
			Revision int64 `json:"revision"`
		}
		if revision.Code != 200 || json.Unmarshal(revision.Body.Bytes(), &result) != nil || result.Revision != want {
			t.Fatalf("unexpected matching revision: %d %s", revision.Code, revision.Body.String())
		}
	}
	checkRevision(0)
	valid := `{"expectedRevision":0,"mode":"title_artist"}`
	for _, tc := range []struct {
		name   string
		body   string
		key    string
		status int
		mutate func(*http.Request)
	}{
		{"anonymous", valid, "bulk-http-invalid", 401, func(r *http.Request) { r.Header.Del("Cookie") }},
		{"csrf", valid, "bulk-http-invalid", 403, func(r *http.Request) { r.Header.Del("X-CSRF-Token") }},
		{"origin", valid, "bulk-http-invalid", 403, func(r *http.Request) { r.Header.Set("Origin", "https://untrusted.invalid") }},
		{"no-key", valid, "", 400, nil},
		{"long-key", valid, strings.Repeat("x", 129), 400, nil},
		{"missing-revision", `{"mode":"title_artist"}`, "bulk-http-invalid", 400, nil},
		{"null-revision", `{"expectedRevision":null,"mode":"title_artist"}`, "bulk-http-invalid", 400, nil},
		{"negative-revision", `{"expectedRevision":-1,"mode":"title_artist"}`, "bulk-http-invalid", 400, nil},
		{"missing-mode", `{"expectedRevision":0}`, "bulk-http-invalid", 400, nil},
		{"unknown-mode", `{"expectedRevision":0,"mode":"auto"}`, "bulk-http-invalid", 400, nil},
		{"missing-score", `{"expectedRevision":0,"mode":"score"}`, "bulk-http-invalid", 400, nil},
		{"null-score", `{"expectedRevision":0,"mode":"score","minimumScore":null}`, "bulk-http-invalid", 400, nil},
		{"string-score", `{"expectedRevision":0,"mode":"score","minimumScore":"35"}`, "bulk-http-invalid", 400, nil},
		{"fraction-score", `{"expectedRevision":0,"mode":"score","minimumScore":35.5}`, "bulk-http-invalid", 400, nil},
		{"high-score", `{"expectedRevision":0,"mode":"score","minimumScore":101}`, "bulk-http-invalid", 400, nil},
		{"negative-score", `{"expectedRevision":0,"mode":"score","minimumScore":-1}`, "bulk-http-invalid", 400, nil},
		{"irrelevant-score", `{"expectedRevision":0,"mode":"title_artist","minimumScore":35}`, "bulk-http-invalid", 400, nil},
		{"unknown-fields", `{"expectedRevision":0,"mode":"title_artist","sourceTrackIds":["private"]}`, "bulk-http-invalid", 400, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := request("POST", "/matching/bulk-merge", tc.body, tc.key, tc.mutate)
			if w.Code != tc.status {
				t.Fatalf("unexpected status %d: %s", w.Code, w.Body.String())
			}
		})
	}
	merged := request("POST", "/matching/bulk-merge", valid, "bulk-http-success", nil)
	if merged.Code != 200 {
		t.Fatalf("bulk merge failed: %d %s", merged.Code, merged.Body.String())
	}
	testkit.ValidateHTTP(t, "BulkMergeResult", merged.Body.Bytes())
	var result matching.BulkMergeResult
	if err := json.Unmarshal(merged.Body.Bytes(), &result); err != nil || result.MergedGroups != 1 || result.MergedSources != 2 || result.SkippedGroups != 0 || result.Revision != 1 {
		t.Fatalf("incorrect owner-scoped result: %+v %v", result, err)
	}
	checkRevision(1)
	replayed := request("POST", "/matching/bulk-merge", valid, "bulk-http-success", nil)
	if replayed.Code != 200 || !bytes.Equal(replayed.Body.Bytes(), merged.Body.Bytes()) {
		t.Fatal("bulk idempotent response changed")
	}
	if w := request("POST", "/matching/bulk-merge", valid, "bulk-http-stale", nil); w.Code != 409 {
		t.Fatal("bulk stale revision accepted")
	}
	if w := request("POST", "/matching/bulk-merge", `{"expectedRevision":0,"mode":"score","minimumScore":35}`, "bulk-http-success", nil); w.Code != 409 {
		t.Fatal("changed operation reused bulk key")
	}
	empty := request("POST", "/matching/bulk-merge", `{"expectedRevision":1,"mode":"score","minimumScore":0}`, "bulk-http-empty", nil)
	if empty.Code != 200 {
		t.Fatal("empty bulk merge failed", empty.Body.String())
	}
	testkit.ValidateHTTP(t, "BulkMergeResult", empty.Body.Bytes())
	if err := json.Unmarshal(empty.Body.Bytes(), &result); err != nil || result.Revision != 1 || result.MergedGroups != 0 || len(result.Changes) != 0 {
		t.Fatal("empty bulk action advanced revision", err)
	}
	for _, id := range privateIDs {
		link, err := store.GetLink(ctx, "other", id)
		if err != nil || link.RecordingID != nil || link.Revision != 0 {
			t.Fatal("other owner's matching state changed", err)
		}
	}
}
