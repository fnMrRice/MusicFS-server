package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/matching"
	"github.com/fnMrRice/MusicFS-server/src/pluginproto"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestMatchingHTTPManualDecisionsCandidatesPrivacyAndAudit(t *testing.T) {
	server, store, _ := libraryHTTP(t)
	cookie, session := login(t, server)
	ctx := context.Background()
	tracks := map[string]library.Track{}
	for _, owner := range []string{"admin", "other"} {
		provider := "mine"
		names := []string{"a", "b", "c"}
		if owner == "other" {
			provider, names = "private", []string{"private"}
		}
		if _, _, err := store.Enqueue(ctx, owner, provider, testkit.ScanPlan(provider), "matching-seed"); err != nil {
			t.Fatal(err)
		}
		job, err := store.Claim(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var items []library.Observation
		for _, name := range names {
			title := "Synthetic recording"
			duration := int64(32000)
			items = append(items, library.Observation{Observation: pluginproto.Observation{ExternalID: name, RootID: provider, RelativePath: "secret-path/" + name + ".wav", DisplayName: name, ObservedVersion: "asset-version", SizeBytes: 100, DurationMs: &duration}, Title: &title, Artists: []string{"Synthetic artist"}})
		}
		if err := store.Publish(ctx, job, testkit.ScanPublication(job, items)); err != nil {
			t.Fatal(err)
		}
		if err := store.Finish(ctx, job, len(items), nil); err != nil {
			t.Fatal(err)
		}
		page, err := store.ListTracks(ctx, owner, provider, "", 50)
		if err != nil {
			t.Fatal(err)
		}
		for _, track := range page.Items {
			tracks[track.DisplayName] = track
		}
	}
	service := matching.New(store, nil)
	server.RegisterMatching(service)
	private, err := service.ManualLink(ctx, "other", matching.LinkRequest{SourceTrackIDs: []string{tracks["private"].ID}}, "private-matching-key")
	if err != nil || private.RecordingID == nil {
		t.Fatal("private recording setup", err)
	}
	request := func(method, path string, body any, key string, mutate func(*http.Request)) *httptest.ResponseRecorder {
		t.Helper()
		var raw []byte
		if text, ok := body.(string); ok {
			raw = []byte(text)
		} else if body != nil {
			raw, _ = json.Marshal(body)
		}
		r := httptest.NewRequest(method, "/api/v1"+path, bytes.NewReader(raw))
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
		if bytes.Contains(w.Body.Bytes(), []byte("secret-path")) || bytes.Contains(w.Body.Bytes(), []byte(`"owner"`)) || bytes.Contains(w.Body.Bytes(), []byte(tracks["private"].ID)) {
			t.Fatal("matching leaked private source or internal fields")
		}
		if w.Code >= 400 {
			testkit.ValidateHTTP(t, "Error", w.Body.Bytes())
		}
		return w
	}
	read := func(path, schema string, target any) {
		t.Helper()
		w := request("GET", path, nil, "", nil)
		if w.Code != 200 {
			t.Fatalf("read %s: %d %s", path, w.Code, w.Body.String())
		}
		testkit.ValidateHTTP(t, schema, w.Body.Bytes())
		if target != nil && json.Unmarshal(w.Body.Bytes(), target) != nil {
			t.Fatal("invalid response")
		}
	}
	var revision struct {
		Revision int64 `json:"revision"`
	}
	read("/matching/revision", "MatchingRevision", &revision)
	if revision.Revision != 0 {
		t.Fatal("another owner's revision leaked")
	}
	ids := []string{tracks["a"].ID, tracks["b"].ID}
	valid := matching.LinkRequest{ExpectedRevision: 0, SourceTrackIDs: ids}
	for _, tc := range []struct {
		name   string
		body   any
		key    string
		status int
		mutate func(*http.Request)
	}{
		{"anonymous", valid, "matching-test-key", 401, func(r *http.Request) { r.Header.Del("Cookie") }},
		{"csrf", valid, "matching-test-key", 403, func(r *http.Request) { r.Header.Del("X-CSRF-Token") }},
		{"origin", valid, "matching-test-key", 403, func(r *http.Request) { r.Header.Set("Origin", "https://untrusted.invalid") }},
		{"revision-required", map[string]any{"sourceTrackIds": ids}, "matching-test-key", 400, nil},
		{"null-revision", map[string]any{"expectedRevision": nil, "sourceTrackIds": ids}, "matching-test-key", 400, nil},
		{"negative-revision", matching.LinkRequest{ExpectedRevision: -1, SourceTrackIDs: ids}, "matching-test-key", 400, nil},
		{"no-key", valid, "", 400, nil}, {"long-key", valid, strings.Repeat("x", 129), 400, nil},
		{"empty-sources", matching.LinkRequest{}, "matching-test-key", 400, nil},
		{"duplicate-sources", matching.LinkRequest{SourceTrackIDs: []string{ids[0], ids[0]}}, "matching-test-key", 400, nil},
		{"private-source", matching.LinkRequest{SourceTrackIDs: []string{tracks["private"].ID}}, "matching-test-key", 404, nil},
		{"private-recording", matching.LinkRequest{SourceTrackIDs: ids, TargetRecordingID: private.RecordingID}, "matching-test-key", 404, nil},
		{"untrusted-evidence", map[string]any{"expectedRevision": 0, "sourceTrackIds": ids, "score": 100}, "matching-test-key", 400, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := request("POST", "/matching/links", tc.body, tc.key, tc.mutate)
			if w.Code != tc.status {
				t.Fatalf("status %d: %s", w.Code, w.Body.String())
			}
		})
	}
	for _, path := range []string{"/recordings/" + *private.RecordingID, "/source-tracks/" + tracks["private"].ID + "/recording-link", "/source-tracks/" + tracks["private"].ID + "/match-candidates"} {
		if w := request("GET", path, nil, "", nil); w.Code != 404 {
			t.Fatalf("private read: %d %s", w.Code, w.Body.String())
		}
	}
	refreshPath := "/source-tracks/" + ids[0] + "/match-candidates/refresh"
	pool := map[string]any{"sourceTrackIds": []string{ids[1]}}
	refresh := func() matching.CandidatePage {
		t.Helper()
		w := request("POST", refreshPath, pool, "", nil)
		if w.Code != 200 {
			t.Fatalf("refresh: %d %s", w.Code, w.Body.String())
		}
		testkit.ValidateHTTP(t, "MatchCandidates", w.Body.Bytes())
		var page matching.CandidatePage
		if json.Unmarshal(w.Body.Bytes(), &page) != nil {
			t.Fatal("invalid candidates")
		}
		return page
	}
	first := refresh()
	if first.Revision != 0 || len(first.Items) != 1 || !first.Items[0].RequiresConfirmation || first.Items[0].Status != "pending" {
		t.Fatalf("missing review candidate: %+v", first)
	}
	second := refresh()
	if len(second.Items) != 1 || second.Items[0].ID != first.Items[0].ID || !second.Items[0].CreatedAt.Equal(first.Items[0].CreatedAt) || second.Revision != first.Revision {
		t.Fatal("same snapshot refresh was not naturally idempotent")
	}
	var link matching.Link
	read("/source-tracks/"+ids[0]+"/recording-link", "RecordingSourceLink", &link)
	if link.RecordingID != nil || link.Decision != "unmatched" {
		t.Fatal("candidate auto-linked a recording")
	}
	for _, body := range []any{map[string]any{"sourceTrackIds": []string{tracks["private"].ID}}, map[string]any{"sourceTrackIds": []string{ids[1], ids[1]}}, map[string]any{"sourceTrackIds": []string{ids[0]}}, map[string]any{"sourceTrackIds": nil}, map[string]any{"snapshot": map[string]any{}}} {
		w := request("POST", refreshPath, body, "", nil)
		if w.Code != 400 && w.Code != 404 {
			t.Fatalf("invalid comparison pool: %d %s", w.Code, w.Body.String())
		}
	}
	for _, path := range []string{"/matching/changes?limit=201", "/matching/changes?after=-1", "/matching/changes?after=abc", "/matching/changes?after=1&after=2", "/source-tracks/" + ids[0] + "/match-candidates?limit=101"} {
		if w := request("GET", path, nil, "", nil); w.Code != 400 {
			t.Fatal("bad matching page accepted", path)
		}
	}
	mutation := func(path string, body any, key string) matching.Mutation {
		t.Helper()
		w := request("POST", path, body, key, nil)
		if w.Code != 200 {
			t.Fatalf("mutation %s: %d %s", path, w.Code, w.Body.String())
		}
		testkit.ValidateHTTP(t, "MatchingMutation", w.Body.Bytes())
		var out matching.Mutation
		if json.Unmarshal(w.Body.Bytes(), &out) != nil {
			t.Fatal("invalid mutation")
		}
		return out
	}
	joined := mutation("/matching/links", valid, "matching-link-key")
	if joined.Revision != 1 || joined.RecordingID == nil || len(joined.Changes) != 2 {
		t.Fatal("link result incomplete")
	}
	replay := mutation("/matching/links", valid, "matching-link-key")
	a, _ := json.Marshal(joined)
	b, _ := json.Marshal(replay)
	if !bytes.Equal(a, b) {
		t.Fatal("manual idempotent replay changed audit")
	}
	if w := request("POST", "/matching/links", valid, "different-key", nil); w.Code != 409 {
		t.Fatal("stale revision accepted")
	}
	if w := request("POST", "/matching/links", matching.LinkRequest{SourceTrackIDs: []string{ids[0]}}, "matching-link-key", nil); w.Code != 409 {
		t.Fatal("changed request reused key")
	}
	if page := refresh(); len(page.Items) != 0 {
		t.Fatal("manually linked pair still offered")
	}
	exclusion := matching.ExclusionRequest{ExpectedRevision: 1, LeftSourceTrackID: ids[0], RightSourceTrackID: ids[1], Excluded: true}
	if w := request("POST", "/matching/exclusions", exclusion, "matching-exclusion-key", nil); w.Code != 409 {
		t.Fatal("exclusion silently split an existing recording")
	}
	undone := mutation("/matching/unlinks", matching.UnlinkRequest{ExpectedRevision: 1, SourceTrackID: ids[1]}, "matching-unlink-key")
	if undone.Revision != 2 || len(undone.Changes) != 1 || undone.Changes[0].FromRecordingID == nil || *undone.Changes[0].FromRecordingID != *joined.RecordingID || undone.Changes[0].ToRecordingID != nil {
		t.Fatal("unlink lost original target")
	}
	read("/source-tracks/"+ids[1]+"/recording-link", "RecordingSourceLink", &link)
	if link.Decision != "unlinked" || link.RecordingID != nil {
		t.Fatal("manual unlink intent missing")
	}
	exclusion.ExpectedRevision = 2
	blocked := mutation("/matching/exclusions", exclusion, "matching-exclusion-key")
	if blocked.Revision != 3 {
		t.Fatal("wrong exclusion revision")
	}
	if page := refresh(); len(page.Items) != 0 {
		t.Fatal("excluded pair offered")
	}
	if w := request("POST", "/matching/links", matching.LinkRequest{ExpectedRevision: 3, SourceTrackIDs: []string{ids[1]}, TargetRecordingID: joined.RecordingID}, "matching-blocked-link", nil); w.Code != 409 {
		t.Fatal("manual link bypassed exclusion")
	}
	exclusion.ExpectedRevision = 3
	exclusion.Excluded = false
	mutation("/matching/exclusions", exclusion, "matching-remove-exclusion")
	mutation("/matching/links", matching.LinkRequest{ExpectedRevision: 4, SourceTrackIDs: []string{ids[1], tracks["c"].ID}, TargetRecordingID: joined.RecordingID}, "matching-rejoin-key")
	split := mutation("/matching/splits", matching.SplitRequest{ExpectedRevision: 5, RecordingID: *joined.RecordingID, SourceTrackIDs: []string{tracks["c"].ID}}, "matching-split-key")
	if split.Revision != 6 || split.RecordingID == nil || *split.RecordingID == *joined.RecordingID || len(split.Changes) != 1 || split.Changes[0].FromRecordingID == nil || *split.Changes[0].FromRecordingID != *joined.RecordingID {
		t.Fatal("split did not retain reversible targets")
	}
	var recording matching.Recording
	read("/recordings/"+*joined.RecordingID, "Recording", &recording)
	if len(recording.SourceTrackIDs) != 2 || recording.Revision != 6 {
		t.Fatal("wrong recording after split")
	}
	read("/recordings/"+*split.RecordingID, "Recording", &recording)
	if len(recording.SourceTrackIDs) != 1 || recording.SourceTrackIDs[0] != tracks["c"].ID {
		t.Fatal("wrong split membership")
	}
	var audit matching.ChangePage
	read("/matching/changes?limit=1", "MatchingChanges", &audit)
	if len(audit.Items) != 1 || audit.NextAfter == nil || audit.Items[0].Sequence != 1 {
		t.Fatal("audit first page missing")
	}
	read("/matching/changes?after=1&limit=200", "MatchingChanges", &audit)
	if len(audit.Items) != 7 || audit.NextAfter != nil || audit.Items[0].Sequence != 2 || audit.Items[6].Sequence != 8 {
		t.Fatalf("audit incomplete: %+v", audit)
	}
	page, err := store.ListTracks(ctx, "admin", "mine", "", 50)
	if err != nil || len(page.Items) != 3 {
		t.Fatal("manual decisions removed sources", err)
	}
	for _, track := range page.Items {
		if track.AssetID != tracks[track.DisplayName].AssetID {
			t.Fatal("manual decision changed source asset")
		}
	}
}
