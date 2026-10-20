package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/enrichment"
	"github.com/fnMrRice/MusicFS-server/src/enrichmentworker"
	"github.com/fnMrRice/MusicFS-server/src/jobs"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/mediastore"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestMetadataHTTPPrivateJobsCandidatesAndInstances(t *testing.T) {
	server, store, _ := libraryHTTP(t)
	cookie, session := login(t, server)
	ctx := context.Background()
	if _, _, err := store.Enqueue(ctx, "other", "843558e1-a158-56ac-834a-baffd12bb5c5", testkit.ScanPlan("843558e1-a158-56ac-834a-baffd12bb5c5"), "metadata-private-source"); err != nil {
		t.Fatal(err)
	}
	scan, err := store.Claim(ctx)
	if err != nil {
		t.Fatal(err)
	}
	item := library.Observation{Observation: pluginproto.Observation{ExternalID: "private", RootID: "843558e1-a158-56ac-834a-baffd12bb5c5", RelativePath: "secret/source.wav", DisplayName: "private", ObservedVersion: "version", SizeBytes: 1}, Artists: []string{}}
	if err := store.Publish(ctx, scan, testkit.ScanPublication(scan, []library.Observation{item})); err != nil {
		t.Fatal(err)
	}
	if err := store.Finish(ctx, scan, 1, nil); err != nil {
		t.Fatal(err)
	}
	tracks, err := store.ListTracks(ctx, "other", "843558e1-a158-56ac-834a-baffd12bb5c5", "", 10)
	if err != nil || len(tracks.Items) != 1 {
		t.Fatal("private source setup", err)
	}
	private, _, err := store.QueueEnrichment(ctx, "other", enrichment.JobRequest{SourceTrackIDs: []string{tracks.Items[0].ID}, InstanceIDs: []string{"15f9b451-bef0-5f89-9d65-e208870fa2ae"}, Fields: []string{"lyrics"}}, "private-metadata-job")
	if err != nil {
		t.Fatal(err)
	}
	work, err := store.ClaimEnrichment(ctx)
	if err != nil {
		t.Fatal(err)
	}
	unsupported := "UNSUPPORTED"
	candidate := enrichment.PreviewCandidate{CandidateID: strings.Repeat("a", 48), Target: work.Target, InstanceID: "15f9b451-bef0-5f89-9d65-e208870fa2ae", Field: "lyrics", Language: "und", Origin: "sidecar", Basis: "exactStem", Status: "unavailable", FailureCode: &unsupported, ExpiresAt: time.Now().UTC().Add(10 * time.Minute)}
	if err := store.ReserveEnrichmentPreview(ctx, work, 0); err != nil {
		t.Fatal(err)
	}
	if err := store.FinishEnrichment(ctx, work, enrichment.Outcome{Candidates: []enrichment.PreviewCandidate{candidate}, Issues: []enrichment.JobIssue{}}); err != nil {
		t.Fatal(err)
	}
	candidateJSON, err := json.Marshal(candidate)
	if err != nil {
		t.Fatal(err)
	}
	testkit.ValidateHTTP(t, "MetadataCandidate", candidateJSON)
	files, err := mediastore.OpenFiles(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { files.Close() })
	lib := library.New(store, jobs.New(store), nil, nil)
	content := mediastore.NewContent(store, store, files)
	service, err := enrichmentworker.New(store, lib, content, []enrichmentworker.Runtime{{ID: "fca57a71-9efe-57dc-b4b9-2094a8c5a3c9", Owner: "admin"}, {ID: "15f9b451-bef0-5f89-9d65-e208870fa2ae", Owner: "other"}}, enrichmentworker.Config{})
	if err != nil {
		t.Fatal(err)
	}
	server.RegisterEnrichment(service)
	server.RegisterMedia(content)
	request := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, "/api/v1"+path, strings.NewReader(body))
		r.AddCookie(cookie)
		r.Header.Set("Origin", origin)
		r.Header.Set("X-CSRF-Token", session.CSRFToken)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Idempotency-Key", "metadata-private-check")
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		if bytes.Contains(w.Body.Bytes(), []byte("secret/source")) || bytes.Contains(w.Body.Bytes(), []byte("15f9b451-bef0-5f89-9d65-e208870fa2ae")) {
			t.Fatal("private scope leaked")
		}
		return w
	}
	w := request("GET", "/scraper-instances", "")
	var instances struct {
		Items []enrichment.Instance `json:"items"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &instances) != nil || len(instances.Items) != 1 || instances.Items[0].InstanceID != "fca57a71-9efe-57dc-b4b9-2094a8c5a3c9" {
		t.Fatal("instances not owner-scoped")
	}
	testkit.ValidateHTTP(t, "ScraperInstances", w.Body.Bytes())
	for _, tc := range []struct{ method, path, body string }{
		{"GET", "/metadata-jobs/" + private.JobID, ""},
		{"GET", "/metadata-jobs/" + private.JobID + "/documents", ""},
		{"POST", "/metadata-jobs/" + private.JobID + "/cancel", "{}"},
		{"POST", "/metadata-candidates/" + candidate.CandidateID + "/apply", `{"expectedRevision":0}`},
		{"POST", "/metadata-jobs", `{"sourceTrackIds":["any-source"],"fields":["lyrics"],"instanceIds":["15f9b451-bef0-5f89-9d65-e208870fa2ae"]}`},
	} {
		w := request(tc.method, tc.path, tc.body)
		if w.Code != http.StatusNotFound {
			t.Fatalf("private route: %d %s", w.Code, w.Body.String())
		}
		testkit.ValidateHTTP(t, "Error", w.Body.Bytes())
	}
	for _, body := range []string{`{}`, `{"expectedRevision":null}`, `{"expectedRevision":0,"automatic":true}`, `{"expectedRevision":0,"sourceTrackId":"other"}`} {
		w := request("POST", "/metadata-candidates/"+candidate.CandidateID+"/apply", body)
		if w.Code != 400 {
			t.Fatal("invalid candidate apply request accepted")
		}
	}
	for _, body := range []string{
		`{"sourceAlbum":null,"fields":["lyrics"]}`,
		`{"sourceAlbum":{"sourceAlbumId":"album"},"fields":["lyrics"]}`,
		`{"sourceAlbum":{"sourceAlbumId":"album","expectedRevision":null},"fields":["lyrics"]}`,
		`{"sourceAlbum":{"sourceAlbumId":"album","expectedRevision":1,"path":"bad"},"fields":["lyrics"]}`,
		`{"sourceAlbum":{"sourceAlbumId":"album","expectedRevision":1},"sourceTrackIds":[],"fields":["lyrics"]}`,
	} {
		if w := request("POST", "/metadata-jobs", body); w.Code != 400 {
			t.Fatalf("invalid album metadata input: %d %s", w.Code, w.Body.String())
		}
	}
}
