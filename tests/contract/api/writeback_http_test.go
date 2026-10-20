package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/jobs"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/mediastore"
	"github.com/fnMrRice/MusicFS-server/src/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/writeback"
	"github.com/fnMrRice/MusicFS-server/src/writebackhost"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestWriteBackHTTPExplicitInputOwnerAndNaturalCancellation(t *testing.T) {
	server, store, _ := libraryHTTP(t)
	cookie, session := login(t, server)
	ctx := context.Background()
	files, err := mediastore.OpenFiles(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { files.Close() })
	content := mediastore.NewContent(store, store, files)
	journal, err := writeback.OpenJournal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { journal.Close() })
	service, err := writebackhost.New(store, library.New(store, jobs.New(store), nil, nil), content, journal, nil, writebackhost.Config{})
	if err != nil {
		t.Fatal(err)
	}
	server.RegisterWriteBack(service)
	queued := map[string]writebackhost.Job{}
	for _, owner := range []string{"admin", "other"} {
		if _, _, err := store.Enqueue(ctx, owner, fixtureInstanceID(owner), testkit.ScanPlan(owner), "writeback-seed-scan"); err != nil {
			t.Fatal(err)
		}
		scan, err := store.Claim(ctx)
		if err != nil {
			t.Fatal(err)
		}
		item := library.Observation{Observation: pluginproto.Observation{ExternalID: "song", RootID: owner, RelativePath: "private-source/song.flac", DisplayName: "song.flac", ObservedVersion: "version-1", SizeBytes: 100}, Artists: []string{}}
		if err := store.Publish(ctx, scan, testkit.ScanPublication(scan, []library.Observation{item})); err != nil {
			t.Fatal(err)
		}
		if err := store.Finish(ctx, scan, 1, nil); err != nil {
			t.Fatal(err)
		}
		page, err := store.ListTracks(ctx, owner, fixtureInstanceID(owner), "", 10)
		if err != nil || len(page.Items) != 1 {
			t.Fatal("missing source", err)
		}
		track := page.Items[0]
		target := mediastore.ContentTarget{SourceTrackID: track.ID, AssetID: track.AssetID, AssetVersion: track.Asset.Version}
		object, err := content.Upload(ctx, owner, mediastore.ContentMetadata{Target: target, Field: "lyrics", Origin: mediastore.Origin{Kind: "upload"}}, "text/plain", "writeback-seed-upload", strings.NewReader("Synthetic lyrics"))
		if err != nil {
			t.Fatal(err)
		}
		selection, err := content.SelectContent(ctx, owner, mediastore.SelectRequest{Target: target, ObjectID: object.ID}, "writeback-seed-selection")
		if err != nil {
			t.Fatal(err)
		}
		input := writebackhost.Request{SourceTrackID: track.ID, AssetID: track.AssetID, AssetVersion: track.Asset.Version, ExpectedRevision: selection.Revision, Fields: []writebackhost.Field{{Name: "lyrics", Language: "und"}}}
		job, _, err := store.QueueWriteback(ctx, owner, input, writeback.Target{SourceTrackID: track.ID, AssetID: track.AssetID, AssetVersion: track.Asset.Version, InstanceID: fixtureInstanceID(owner), RootID: owner, RelativePath: item.RelativePath, FileIdentity: "file-identity"}, "writeback-seed-job")
		if err != nil {
			t.Fatal(err)
		}
		queued[owner] = job
	}
	request := func(method, path, body, key string, change func(*http.Request)) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, "/api/v1"+path, strings.NewReader(body))
		r.AddCookie(cookie)
		r.Header.Set("Origin", origin)
		r.Header.Set("X-CSRF-Token", session.CSRFToken)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Idempotency-Key", key)
		if change != nil {
			change(r)
		}
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		if bytes.Contains(w.Body.Bytes(), []byte("private-source")) || bytes.Contains(w.Body.Bytes(), []byte("file-identity")) || bytes.Contains(w.Body.Bytes(), []byte(`"owner"`)) {
			t.Fatal("private writeback state leaked")
		}
		if w.Code >= 400 {
			testkit.ValidateHTTP(t, "Error", w.Body.Bytes())
		}
		return w
	}
	own := queued["admin"]
	encoded, _ := json.Marshal(own.Request)
	valid := string(encoded)
	for _, tc := range []struct {
		name, body, key string
		status          int
		change          func(*http.Request)
	}{
		{"disabled", valid, "new-disabled-write", 403, nil},
		{"missing-key", valid, "", 400, nil},
		{"anonymous", valid, "writeback-auth-check", 401, func(r *http.Request) { r.Header.Del("Cookie") }},
		{"csrf", valid, "writeback-auth-check", 403, func(r *http.Request) { r.Header.Del("X-CSRF-Token") }},
		{"origin", valid, "writeback-auth-check", 403, func(r *http.Request) { r.Header.Set("Origin", "https://untrusted.invalid") }},
		{"missing-fields", `{}`, "invalid-write-input", 400, nil},
		{"path", strings.TrimSuffix(valid, "}") + `,"path":"private-source/song.flac"}`, "invalid-write-input", 400, nil},
		{"bytes", strings.TrimSuffix(valid, "}") + `,"bytes":"YWJj"}`, "invalid-write-input", 400, nil},
		{"no-overwrite", strings.Replace(valid, `,"overwrite":false`, "", 1), "invalid-write-input", 400, nil},
		{"null-overwrite", strings.Replace(valid, `"overwrite":false`, `"overwrite":null`, 1), "invalid-write-input", 400, nil},
		{"negative-revision", strings.Replace(valid, `"expectedRevision":1`, `"expectedRevision":-1`, 1), "invalid-write-input", 400, nil},
		{"two-languages", strings.Replace(valid, `[{"name":"lyrics","language":"und"}]`, `[{"name":"lyrics","language":"und"},{"name":"lyrics","language":"en"}]`, 1), "invalid-write-input", 400, nil},
		{"missing-language", strings.Replace(valid, `,"language":"und"`, "", 1), "invalid-write-input", 400, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := request("POST", "/writeback-jobs", tc.body, tc.key, tc.change)
			if w.Code != tc.status {
				t.Fatalf("status %d: %s", w.Code, w.Body.String())
			}
		})
	}
	w := request("POST", "/writeback-jobs", valid, "writeback-seed-job", nil)
	if w.Code != 202 {
		t.Fatalf("replay: %d %s", w.Code, w.Body.String())
	}
	testkit.ValidateHTTP(t, "WriteBackAccepted", w.Body.Bytes())
	var accepted struct {
		JobID  string `json:"jobId"`
		Merged bool   `json:"merged"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &accepted)
	if accepted.JobID != own.ID || !accepted.Merged {
		t.Fatal("create did not replay original job")
	}
	for _, action := range []string{"", "/cancel", "/recover"} {
		method := "GET"
		if action != "" {
			method = "POST"
		}
		w := request(method, "/writeback-jobs/"+queued["other"].ID+action, `{}`, "", nil)
		if w.Code != 404 {
			t.Fatalf("private %s: %d %s", action, w.Code, w.Body.String())
		}
	}
	w = request("GET", "/writeback-jobs/"+own.ID, "", "", nil)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	testkit.ValidateHTTP(t, "WriteBackJob", w.Body.Bytes())
	w = request("POST", "/writeback-jobs/"+own.ID+"/recover", `{}`, "", nil)
	if w.Code != 409 {
		t.Fatal("queued recovery accepted")
	}
	var first writebackhost.Job
	for i := range 2 {
		w = request("POST", "/writeback-jobs/"+own.ID+"/cancel", `{}`, "", nil)
		if w.Code != 200 {
			t.Fatalf("cancel: %d %s", w.Code, w.Body.String())
		}
		testkit.ValidateHTTP(t, "WriteBackJob", w.Body.Bytes())
		var cancelled writebackhost.Job
		_ = json.Unmarshal(w.Body.Bytes(), &cancelled)
		if cancelled.Status != "cancelled" || cancelled.ID != own.ID {
			t.Fatal("wrong cancelled job")
		}
		if i == 0 {
			first = cancelled
		} else if !cancelled.UpdatedAt.Equal(first.UpdatedAt) {
			t.Fatal("repeat cancellation changed persisted job")
		}
	}
	w = request("POST", "/writeback-jobs/"+own.ID+"/recover", `{}`, "", nil)
	if w.Code != 409 {
		t.Fatal("cancelled recovery accepted")
	}
	w = request("POST", "/writeback-jobs/"+own.ID+"/cancel", `{"path":"bad"}`, "", nil)
	if w.Code != 400 {
		t.Fatal("cancel accepted unknown body")
	}
}
