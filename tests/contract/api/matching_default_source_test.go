package api_test

import (
	"encoding/json"
	"fmt"
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

func TestDefaultSourceHTTPRequiredChoiceRevisionAndGroupedResult(t *testing.T) {
	server, store, _ := libraryHTTP(t)
	cookie, session := login(t, server)
	server.RegisterMatching(matching.New(store, nil, standard.Plugin{}))
	ctx := t.Context()
	provider := "316664dc-7106-5acd-8df7-88c9d0d9baef"
	if _, _, err := store.Enqueue(ctx, "admin", provider, testkit.ScanPlan(provider), "default-http-seed"); err != nil {
		t.Fatal(err)
	}
	job, err := store.Claim(ctx)
	if err != nil {
		t.Fatal(err)
	}
	items := []library.Observation{}
	for i, name := range []string{"a", "b"} {
		rate := int64(128000 + 192000*i)
		items = append(items, library.Observation{Observation: pluginproto.Observation{ExternalID: name, RootID: provider, RelativePath: "secret-path/" + name + ".mp3", DisplayName: name, ObservedVersion: "v1", SizeBytes: 100, BitrateBps: &rate}})
	}
	if err = store.Publish(ctx, job, testkit.ScanPublication(job, items)); err != nil {
		t.Fatal(err)
	}
	if err = store.Finish(ctx, job, len(items), nil); err != nil {
		t.Fatal(err)
	}
	page, err := store.ListTracks(ctx, "admin", provider, "", 100)
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{page.Items[0].ID, page.Items[1].ID}
	linked, err := store.ManualLink(ctx, "admin", matching.LinkRequest{SourceTrackIDs: ids}, "default-http-link")
	if err != nil {
		t.Fatal(err)
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
		if strings.Contains(w.Body.String(), "secret-path") || strings.Contains(w.Body.String(), `"owner"`) {
			t.Fatal("default selection leaked internal fields")
		}
		if w.Code >= 400 {
			testkit.ValidateHTTP(t, "Error", w.Body.Bytes())
		}
		return w
	}
	valid := fmt.Sprintf(`{"expectedRevision":1,"recordingId":%q,"sourceTrackId":%q}`, *linked.RecordingID, ids[0])
	testkit.ValidateHTTP(t, "DefaultSourceRequest", []byte(valid))
	for _, tc := range []struct {
		name, body, key string
		status          int
		mutate          func(*http.Request)
	}{
		{"anonymous", valid, "default-http-change", 401, func(r *http.Request) { r.Header.Del("Cookie") }},
		{"csrf", valid, "default-http-change", 403, func(r *http.Request) { r.Header.Del("X-CSRF-Token") }},
		{"origin", valid, "default-http-change", 403, func(r *http.Request) { r.Header.Set("Origin", "https://untrusted.invalid") }},
		{"no-key", valid, "", 400, nil},
		{"missing-revision", fmt.Sprintf(`{"recordingId":%q,"sourceTrackId":null}`, *linked.RecordingID), "default-http-change", 400, nil},
		{"missing-choice", fmt.Sprintf(`{"expectedRevision":1,"recordingId":%q}`, *linked.RecordingID), "default-http-change", 400, nil},
		{"invalid-choice", fmt.Sprintf(`{"expectedRevision":1,"recordingId":%q,"sourceTrackId":false}`, *linked.RecordingID), "default-http-change", 400, nil},
		{"empty-choice", fmt.Sprintf(`{"expectedRevision":1,"recordingId":%q,"sourceTrackId":""}`, *linked.RecordingID), "default-http-change", 400, nil},
		{"unknown-field", strings.TrimSuffix(valid, "}") + `,"owner":"other"}`, "default-http-change", 400, nil},
		{"stale", strings.Replace(valid, `"expectedRevision":1`, `"expectedRevision":0`, 1), "default-http-change", 409, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := request("POST", "/matching/default-source", tc.body, tc.key, tc.mutate)
			if w.Code != tc.status {
				t.Fatalf("unexpected status: %d %s", w.Code, w.Body.String())
			}
		})
	}
	w := request("POST", "/matching/default-source", valid, "default-http-change", nil)
	if w.Code != 200 {
		t.Fatalf("select default: %d %s", w.Code, w.Body.String())
	}
	testkit.ValidateHTTP(t, "MatchingMutation", w.Body.Bytes())
	replay := request("POST", "/matching/default-source", valid, "default-http-change", nil)
	if replay.Code != 200 || replay.Body.String() != w.Body.String() {
		t.Fatal("HTTP replay changed mutation")
	}
	assertGroup := func(want string, manual bool, revision int64) {
		t.Helper()
		result := request("GET", "/source-tracks?groupBy=recording", "", "", nil)
		if result.Code != 200 {
			t.Fatalf("group read: %d %s", result.Code, result.Body.String())
		}
		testkit.ValidateHTTP(t, "SourceTracks", result.Body.Bytes())
		var current library.Page
		if err := json.Unmarshal(result.Body.Bytes(), &current); err != nil {
			t.Fatal(err)
		}
		if len(current.Items) != 1 || current.Items[0].ID != want || (current.Items[0].ManualSourceTrackID != nil) != manual || current.MatchingRevision == nil || *current.MatchingRevision != revision {
			t.Fatalf("unexpected default group: %+v", current)
		}
	}
	assertGroup(ids[0], true, 2)
	clear := fmt.Sprintf(`{"expectedRevision":2,"recordingId":%q,"sourceTrackId":null}`, *linked.RecordingID)
	testkit.ValidateHTTP(t, "DefaultSourceRequest", []byte(clear))
	w = request("POST", "/matching/default-source", clear, "default-http-clear", nil)
	if w.Code != 200 {
		t.Fatalf("clear default: %d %s", w.Code, w.Body.String())
	}
	testkit.ValidateHTTP(t, "MatchingMutation", w.Body.Bytes())
	assertGroup(ids[1], false, 3)
}
