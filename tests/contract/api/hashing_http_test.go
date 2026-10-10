package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/identification"
	"github.com/fnMrRice/MusicFS-server/src/identification/hashworker"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/pluginproto"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

type hashHTTPSource map[string]identification.HashTarget

func (s hashHTTPSource) ResolveHashTarget(_ context.Context, owner, track string) (identification.HashTarget, error) {
	if target, ok := s[owner+"/"+track]; ok {
		return target, nil
	}
	return identification.HashTarget{}, domain.Fail("NOT_FOUND", "Source track not found")
}
func (hashHTTPSource) OpenStableHash(context.Context, string, string, identification.HashTarget) (identification.StableInput, error) {
	return nil, domain.Fail("UNSUPPORTED", "No test reader")
}
func (hashHTTPSource) AutoHashScopes() []identification.HashScope { return nil }

// This control-plane test uses real SQLite and authentication, but deliberately
// leaves workers stopped so queued cancellation and merging are deterministic.
func TestHashHTTPAuthorizationIdempotencyAndCancel(t *testing.T) {
	server, store, _ := libraryHTTP(t)
	cookie, session := login(t, server)
	ctx := context.Background()
	source := hashHTTPSource{}
	publish := func(owner, provider, version string) identification.HashTarget {
		t.Helper()
		if _, _, err := store.Enqueue(ctx, owner, provider, testkit.ScanPlan(provider), "hash-scan-"+version); err != nil {
			t.Fatal(err)
		}
		job, err := store.Claim(ctx)
		if err != nil {
			t.Fatal(err)
		}
		observation := library.Observation{Observation: pluginproto.Observation{ExternalID: "track", RootID: provider,
			RelativePath: "private-path.wav", DisplayName: "track.wav", MIME: "audio/wav", ObservedVersion: version, SizeBytes: 4}, Artists: []string{}}
		if err := store.Publish(ctx, job, testkit.ScanPublication(job, []library.Observation{observation})); err != nil {
			t.Fatal(err)
		}
		if err := store.Finish(ctx, job, 1, nil); err != nil {
			t.Fatal(err)
		}
		page, err := store.ListTracks(ctx, owner, provider, "", 50)
		if err != nil || len(page.Items) != 1 {
			t.Fatal("missing source track", err)
		}
		track := page.Items[0]
		target := identification.HashTarget{SourceTrackID: track.ID, AssetID: track.AssetID, AssetVersion: track.Asset.Version, FileSizeBytes: 4}
		source[owner+"/"+track.ID] = target
		return target
	}
	mine := publish("admin", "mine", "version-1")
	other := publish("other", "private", "version-1")
	service, err := hashworker.New(store, source, hashworker.Config{MaxConcurrent: 1, BytesPerSecond: 4194304})
	if err != nil {
		t.Fatal(err)
	}
	server.RegisterHashing(service)
	private, _, err := service.Submit(ctx, "other", other.SourceTrackID, "private-hash-key")
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
		if strings.Contains(w.Body.String(), "private-path") || strings.Contains(w.Body.String(), "Ticket") || strings.Contains(w.Body.String(), "Owner") {
			t.Fatal("hash control response leaked internal data")
		}
		if w.Code >= 400 {
			testkit.ValidateHTTP(t, "Error", w.Body.Bytes())
		}
		return w
	}
	createPath := "/source-tracks/" + mine.SourceTrackID + "/hash-jobs"
	for _, tc := range []struct {
		name, body, key string
		status          int
		mutate          func(*http.Request)
	}{
		{"anonymous", "{}", "hash-key-0001", 401, func(r *http.Request) { r.Header.Del("Cookie") }},
		{"csrf", "{}", "hash-key-0001", 403, func(r *http.Request) { r.Header.Del("X-CSRF-Token") }},
		{"origin", "{}", "hash-key-0001", 403, func(r *http.Request) { r.Header.Set("Origin", "https://untrusted.invalid") }},
		{"unknown", `{"range":{"start":1}}`, "hash-key-0001", 400, nil},
		{"null", "null", "hash-key-0001", 400, nil}, {"empty", "", "hash-key-0001", 400, nil},
		{"missing key", "{}", "", 400, nil}, {"long key", "{}", strings.Repeat("x", 129), 400, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if w := request("POST", createPath, tc.body, tc.key, tc.mutate); w.Code != tc.status {
				t.Fatalf("expected %d: %d %s", tc.status, w.Code, w.Body.String())
			}
		})
	}
	w := request("POST", createPath, "{}", "hash-key-0001", nil)
	if w.Code != 202 {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	testkit.ValidateHTTP(t, "HashAccepted", w.Body.Bytes())
	var accepted struct {
		ID     string `json:"jobId"`
		Merged bool   `json:"merged"`
	}
	if json.Unmarshal(w.Body.Bytes(), &accepted) != nil || accepted.Merged {
		t.Fatal("new task unexpectedly merged")
	}
	for _, key := range []string{"hash-key-0001", "hash-key-0002"} {
		w = request("POST", createPath, "{}", key, nil)
		var replay struct {
			ID     string `json:"jobId"`
			Merged bool   `json:"merged"`
		}
		if w.Code != 202 || json.Unmarshal(w.Body.Bytes(), &replay) != nil || replay.ID != accepted.ID || !replay.Merged {
			t.Fatalf("task replay/merge failed: %s", w.Body.String())
		}
	}
	for _, path := range []string{"/hash-jobs/" + private.ID, "/hash-jobs/absent"} {
		for _, method := range []string{"GET", "POST"} {
			endpoint := path
			if method == "POST" {
				endpoint += "/cancel"
			}
			if w := request(method, endpoint, "{}", "", nil); w.Code != 404 {
				t.Fatalf("foreign or absent hash job disclosed: %d %s", w.Code, w.Body.String())
			}
		}
	}
	if w := request("POST", "/source-tracks/"+other.SourceTrackID+"/hash-jobs", "{}", "foreign-hash-key", nil); w.Code != 404 {
		t.Fatal("foreign source hash was accepted")
	}
	jobPath := "/hash-jobs/" + accepted.ID
	if w := request("GET", jobPath, "", "", func(r *http.Request) { r.Header.Del("Cookie") }); w.Code != 401 {
		t.Fatal("anonymous hash status read was accepted")
	}
	if w := request("POST", jobPath+"/cancel", `{"force":true}`, "", nil); w.Code != 400 {
		t.Fatal("cancel accepted unsupported force semantics")
	}
	if _, err := store.DB.ExecContext(ctx, "UPDATE idempotency SET expires_at=? WHERE owner=? AND route=? AND key=?", "2000-01-01T00:00:00Z", "admin", "hash/"+mine.SourceTrackID, "hash-key-0002"); err != nil {
		t.Fatal(err)
	}
	if w := request("POST", createPath, "{}", "hash-key-0002", nil); w.Code != 410 {
		t.Fatalf("expired hash operation did not return Gone: %d %s", w.Code, w.Body.String())
	}
	if w := request("POST", jobPath+"/cancel", "{}", "", func(r *http.Request) { r.Header.Del("X-CSRF-Token") }); w.Code != 403 {
		t.Fatal("cancel did not enforce mutation authorization")
	}
	var previous identification.HashJob
	for i, method := range []string{"POST", "POST", "GET"} {
		endpoint := jobPath
		if method == "POST" {
			endpoint += "/cancel"
		}
		w = request(method, endpoint, "{}", "", nil)
		if w.Code != 200 {
			t.Fatalf("cancel/get: %d %s", w.Code, w.Body.String())
		}
		testkit.ValidateHTTP(t, "HashJob", w.Body.Bytes())
		var job identification.HashJob
		if json.Unmarshal(w.Body.Bytes(), &job) != nil || job.Status != "cancelled" || job.Error == nil || job.Error.RequestID != w.Header().Get("X-Request-ID") {
			t.Fatal("cancelled state or per-request error identity missing")
		}
		if i > 0 && (!job.UpdatedAt.Equal(previous.UpdatedAt) || job.Progress != previous.Progress || job.ID != previous.ID) {
			t.Fatal("repeated cancellation changed terminal state")
		}
		previous = job
	}
	publish("admin", "mine", "version-2")
	if w := request("POST", createPath, "{}", "hash-key-0001", nil); w.Code != 409 {
		t.Fatalf("changed asset reused an idempotency key: %d %s", w.Code, w.Body.String())
	}
}
