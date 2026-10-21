package api_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/metadata/identification"
	"github.com/fnMrRice/MusicFS-server/src/metadata/identification/fingerprint"
	"github.com/fnMrRice/MusicFS-server/src/metadata/identification/fingerprintworker"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

type unusedFingerprintExtractor struct{ identity fingerprint.Identity }

func (e unusedFingerprintExtractor) Identity() fingerprint.Identity { return e.identity }
func (unusedFingerprintExtractor) Extract(context.Context, fingerprint.Request, io.ReaderAt, int64) (fingerprint.Detail, error) {
	return fingerprint.Detail{}, domain.Fail("UNSUPPORTED", "Extractor is deliberately stopped")
}

func fingerprintTestIdentity() fingerprint.Identity {
	id := fingerprint.Identity{Algorithm: "chromaprint", AlgorithmVersion: "1.6.1", AlgorithmConfig: "cli-2-raw-v1", NormalizationRevision: audio.NormalizationRevision, QualityRevision: fingerprint.QualityRevision,
		Fpcalc: fingerprint.ToolIdentity{Version: "fpcalc version 1.6.1 (test)", SHA256: strings.Repeat("a", 64)}, Decoder: fingerprint.ToolIdentity{Version: "ffmpeg test", SHA256: strings.Repeat("b", 64)}}
	b, _ := json.Marshal(id)
	id.ConfigRevision = fmt.Sprintf("sha256-%x", sha256.Sum256(b))
	return id
}

// Real SQLite and session/CSRF handlers; stopped workers make queued merging and
// cancellation deterministic. Actual external tools are covered by server E2E.
func TestFingerprintHTTPAuthorizationRangesIdempotencyAndCancel(t *testing.T) {
	server, store, _ := libraryHTTP(t)
	cookie, session := login(t, server)
	ctx := context.Background()
	source := hashHTTPSource{}
	publish := func(owner, provider string) identification.HashTarget {
		t.Helper()
		if _, _, err := store.Enqueue(ctx, owner, provider, testkit.ScanPlan(provider), "fingerprint-scan-key"); err != nil {
			t.Fatal(err)
		}
		job, err := store.Claim(ctx)
		if err != nil {
			t.Fatal(err)
		}
		item := library.Observation{Observation: pluginproto.Observation{ExternalID: "track", RootID: provider, RelativePath: "private-fingerprint-path.wav", DisplayName: "source.wav", MIME: "audio/wav", ObservedVersion: "version-1", SizeBytes: 4}, Artists: []string{}}
		if err := store.Publish(ctx, job, testkit.ScanPublication(job, []library.Observation{item})); err != nil {
			t.Fatal(err)
		}
		if err := store.Finish(ctx, job, 1, nil); err != nil {
			t.Fatal(err)
		}
		page, err := store.ListTracks(ctx, owner, provider, "", 50)
		if err != nil || len(page.Items) != 1 {
			t.Fatal("missing track", err)
		}
		track := page.Items[0]
		target := identification.HashTarget{SourceTrackID: track.ID, AssetID: track.AssetID, AssetVersion: track.Asset.Version, FileSizeBytes: 4}
		source[owner+"/"+track.ID] = target
		return target
	}
	mine, other := publish("admin", "316664dc-7106-5acd-8df7-88c9d0d9baef"), publish("other", "843558e1-a158-56ac-834a-baffd12bb5c5")
	files, err := mediastore.OpenFiles(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { files.Close() })
	media, err := mediastore.New(store, files)
	if err != nil {
		t.Fatal(err)
	}
	service, err := fingerprintworker.New(store, source, unusedFingerprintExtractor{fingerprintTestIdentity()}, media, fingerprintworker.Config{MaxConcurrent: 1})
	if err != nil {
		t.Fatal(err)
	}
	server.RegisterFingerprinting(service)
	private, _, err := service.Submit(ctx, "other", other.SourceTrackID, "private-fingerprint-key", 0, 10000)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := store.ClaimFingerprint(ctx)
	if err != nil || claimed.ID != private.ID {
		t.Fatal("private task claim failed", err)
	}
	detail := fingerprint.Detail{SchemaVersion: 1, Identity: fingerprintTestIdentity(), RequestedDurationMs: 10000,
		PCM: audio.PCMInfo{Format: audio.FingerprintPCM, NormalizationRevision: audio.NormalizationRevision, SampleCount: 11025, DecoderVersion: "ffmpeg test", DecoderSHA256: strings.Repeat("b", 64)}, Values: []uint32{1, 2}}
	detail.Quality = fingerprint.Assess(detail.Values, detail.PCM.SampleCount)
	rawDetail, err := detail.Encode()
	if err != nil {
		t.Fatal(err)
	}
	summary, _ := json.Marshal(identification.FingerprintSummary{ExtractorIdentity: detail.Identity, SampleCount: detail.PCM.SampleCount, ValueCount: len(detail.Values), Quality: detail.Quality, ComputedAt: time.Now().UTC()})
	_, err = media.Publish(ctx, "other", "fingerprint", "application/json", mediastore.Target{Purpose: "fingerprint", ID: claimed.ID, Generation: claimed.Generation, Metadata: summary}, strings.NewReader(string(rawDetail)), fingerprint.MaxDetailBytes, func(io.ReadSeeker) error { return nil }, func(publish func() error) error { return publish() })
	if err != nil {
		t.Fatal(err)
	}
	private, err = service.Get(ctx, "other", private.ID)
	if err != nil || private.Result == nil {
		t.Fatal("private index not published", err)
	}
	if _, err := service.Detail(ctx, "other", private.Result.ID); err != nil {
		t.Fatal("owner cannot read private detail", err)
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
		if strings.Contains(w.Body.String(), "private-fingerprint-path") || strings.Contains(w.Body.String(), "relativePath") || strings.Contains(w.Body.String(), "Owner") {
			t.Fatal("private data in response")
		}
		if w.Code >= 400 {
			testkit.ValidateHTTP(t, "Error", w.Body.Bytes())
		}
		return w
	}
	const valid = `{"startMs":0,"durationMs":10000}`
	create := "/source-tracks/" + mine.SourceTrackID + "/fingerprint-jobs"
	for _, tc := range []struct {
		name, body, key string
		status          int
		mutate          func(*http.Request)
	}{
		{"anonymous", valid, "fingerprint-key", 401, func(r *http.Request) { r.Header.Del("Cookie") }},
		{"csrf", valid, "fingerprint-key", 403, func(r *http.Request) { r.Header.Del("X-CSRF-Token") }},
		{"origin", valid, "fingerprint-key", 403, func(r *http.Request) { r.Header.Set("Origin", "https://untrusted.invalid") }},
		{"missing", `{}`, "fingerprint-key", 400, nil}, {"null", `null`, "fingerprint-key", 400, nil},
		{"missing-start", `{"durationMs":10000}`, "fingerprint-key", 400, nil}, {"null-start", `{"startMs":null,"durationMs":10000}`, "fingerprint-key", 400, nil},
		{"short", `{"startMs":0,"durationMs":9999}`, "fingerprint-key", 400, nil}, {"long", `{"startMs":0,"durationMs":120001}`, "fingerprint-key", 400, nil},
		{"negative", `{"startMs":-1,"durationMs":10000}`, "fingerprint-key", 400, nil}, {"late", `{"startMs":86400001,"durationMs":10000}`, "fingerprint-key", 400, nil},
		{"float", `{"startMs":0.5,"durationMs":10000}`, "fingerprint-key", 400, nil}, {"upload", `{"startMs":0,"durationMs":10000,"upload":true}`, "fingerprint-key", 400, nil},
		{"no-key", valid, "", 400, nil}, {"long-key", valid, strings.Repeat("k", 129), 400, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if w := request("POST", create, tc.body, tc.key, tc.mutate); w.Code != tc.status {
				t.Fatalf("status %d: %s", w.Code, w.Body.String())
			}
		})
	}
	w := request("POST", create, valid, "fingerprint-key", nil)
	if w.Code != 202 {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	testkit.ValidateHTTP(t, "FingerprintAccepted", w.Body.Bytes())
	var accepted struct {
		JobID  string `json:"jobId"`
		Merged bool   `json:"merged"`
	}
	if json.Unmarshal(w.Body.Bytes(), &accepted) != nil || accepted.Merged {
		t.Fatal("bad accepted task")
	}
	for _, key := range []string{"fingerprint-key", "fingerprint-new-key"} {
		w = request("POST", create, valid, key, nil)
		var replay struct {
			JobID  string `json:"jobId"`
			Merged bool   `json:"merged"`
		}
		if w.Code != 202 || json.Unmarshal(w.Body.Bytes(), &replay) != nil || replay.JobID != accepted.JobID || !replay.Merged {
			t.Fatalf("replay: %s", w.Body.String())
		}
	}
	if w = request("POST", create, `{"startMs":1,"durationMs":10000}`, "fingerprint-key", nil); w.Code != 409 {
		t.Fatal("changed range reused idempotency key")
	}
	for _, id := range []string{private.ID, "absent"} {
		for _, method := range []string{"GET", "POST"} {
			path := "/fingerprint-jobs/" + id
			if method == "POST" {
				path += "/cancel"
			}
			if w = request(method, path, `{}`, "", nil); w.Code != 404 {
				t.Fatal("foreign/absent job disclosed")
			}
		}
	}
	for _, path := range []string{"/source-tracks/" + other.SourceTrackID + "/fingerprints", "/fingerprints/" + private.Result.ID, "/fingerprints/" + domain.NewID()} {
		if w = request("GET", path, "", "", nil); w.Code != 404 {
			t.Fatalf("private index disclosed: %d %s", w.Code, w.Body.String())
		}
	}
	list := "/source-tracks/" + mine.SourceTrackID + "/fingerprints"
	if w = request("GET", list, "", "", nil); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	testkit.ValidateHTTP(t, "FingerprintIndexes", w.Body.Bytes())
	for _, q := range []string{"?limit=0", "?limit=101", "?limit=", "?limit=1&limit=2", "?after=a&after=b", "?after=" + strings.Repeat("x", 257)} {
		if w = request("GET", list+q, "", "", nil); w.Code != 400 {
			t.Fatalf("pagination accepted: %s", q)
		}
	}
	jobPath := "/fingerprint-jobs/" + accepted.JobID
	if w = request("POST", jobPath+"/cancel", `{"force":true}`, "", nil); w.Code != 400 {
		t.Fatal("cancel accepted force")
	}
	if w = request("POST", jobPath+"/cancel", `{}`, "", func(r *http.Request) { r.Header.Del("X-CSRF-Token") }); w.Code != 403 {
		t.Fatal("cancel missed CSRF")
	}
	var previous identification.FingerprintJob
	for i, method := range []string{"POST", "POST", "GET"} {
		path := jobPath
		if method == "POST" {
			path += "/cancel"
		}
		w = request(method, path, `{}`, "", nil)
		if w.Code != 200 {
			t.Fatal(w.Body.String())
		}
		testkit.ValidateHTTP(t, "FingerprintJob", w.Body.Bytes())
		var job identification.FingerprintJob
		if json.Unmarshal(w.Body.Bytes(), &job) != nil || job.Status != "cancelled" || job.Error == nil || job.Error.RequestID != w.Header().Get("X-Request-ID") {
			t.Fatal("cancel state/request identity missing")
		}
		if i > 0 && (!previous.UpdatedAt.Equal(job.UpdatedAt) || previous.ID != job.ID) {
			t.Fatal("repeat cancel mutated terminal state")
		}
		previous = job
	}
}

func TestFingerprintHTTPDisabled(t *testing.T) {
	server, _, _ := libraryHTTP(t)
	cookie, session := login(t, server)
	server.RegisterFingerprinting(nil)
	for _, tc := range []struct{ method, path, body string }{
		{"POST", "/source-tracks/absent/fingerprint-jobs", `{"startMs":0,"durationMs":10000}`},
		{"GET", "/fingerprint-jobs/absent", ""}, {"POST", "/fingerprint-jobs/absent/cancel", `{}`},
		{"GET", "/source-tracks/absent/fingerprints", ""}, {"GET", "/fingerprints/absent", ""},
	} {
		r := httptest.NewRequest(tc.method, "/api/v1"+tc.path, strings.NewReader(tc.body))
		r.AddCookie(cookie)
		r.Header.Set("Origin", origin)
		r.Header.Set("X-CSRF-Token", session.CSRFToken)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		if w.Code != 415 || !strings.Contains(w.Body.String(), "UNSUPPORTED") {
			t.Fatalf("disabled: %d %s", w.Code, w.Body.String())
		}
	}
}
