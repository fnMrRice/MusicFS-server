package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/app/api"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/library/jobs"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func libraryHTTP(t *testing.T) (*api.Server, *sqlite.Store, *http.Cookie) {
	t.Helper()
	ctx := context.Background()
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	server, _ := testServer()
	queue := jobs.New(store)
	service := library.New(store, queue, []library.Runtime{{ID: "316664dc-7106-5acd-8df7-88c9d0d9baef", Owner: "admin", Name: "My local library"}, {ID: "843558e1-a158-56ac-834a-baffd12bb5c5", Owner: "other", Name: "Other library"}}, nil)
	server.RegisterLibrary(service)
	cookie, _ := login(t, server)
	return server, store, cookie
}

func TestLibraryHTTPAuthorizationAndModel(t *testing.T) {
	server, store, cookie := libraryHTTP(t)
	ctx := context.Background()
	for _, owner := range []string{"admin", "other"} {
		provider := "316664dc-7106-5acd-8df7-88c9d0d9baef"
		if owner == "other" {
			provider = "843558e1-a158-56ac-834a-baffd12bb5c5"
		}
		if _, _, err := store.Enqueue(ctx, owner, provider, testkit.ScanPlan(provider), "scan-test-key"); err != nil {
			t.Fatal(err)
		}
		job, err := store.Claim(ctx)
		if err != nil {
			t.Fatal(err)
		}
		item := library.Observation{Observation: pluginproto.Observation{
			ExternalID: "one", RootID: provider, RelativePath: "private/path/中文.wav", ObservedVersion: "version-1", DisplayName: "中文.wav", MIME: "audio/wav", SizeBytes: 100}, Artists: []string{}}
		if err := store.Publish(ctx, job, testkit.ScanPublication(job, []library.Observation{item})); err != nil {
			t.Fatal(err)
		}
		if err := store.Finish(ctx, job, 1, nil); err != nil {
			t.Fatal(err)
		}
	}
	for _, test := range []struct {
		path   string
		schema string
		status int
	}{{"/api/v1/provider-instances", "Providers", 200}, {"/api/v1/source-tracks", "SourceTracks", 200},
		{"/api/v1/source-tracks?providerInstanceId=843558e1-a158-56ac-834a-baffd12bb5c5", "Error", 404},
		{"/api/v1/source-tracks?limit=201", "Error", 400}, {"/api/v1/source-tracks?limit=", "Error", 400},
		{"/api/v1/source-tracks?limit=abc", "Error", 400}, {"/api/v1/source-tracks?cursor=expired", "Error", 410}} {
		t.Run(test.path, func(t *testing.T) {
			r := httptest.NewRequest("GET", test.path, nil)
			r.AddCookie(cookie)
			w := httptest.NewRecorder()
			server.ServeHTTP(w, r)
			if w.Code != test.status {
				t.Fatalf("status %d: %s", w.Code, w.Body.String())
			}
			testkit.ValidateHTTP(t, test.schema, w.Body.Bytes())
			if strings.Contains(w.Body.String(), "private") {
				t.Fatal("private provider or source path leaked")
			}
			if test.path == "/api/v1/source-tracks" {
				var page library.Page
				if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil || len(page.Items) != 1 || page.Items[0].ProviderID != "316664dc-7106-5acd-8df7-88c9d0d9baef" {
					t.Fatal("wrong authorized library page", err)
				}
			}
		})
	}
}

func TestJobHTTPErrorContractAndOwnership(t *testing.T) {
	server, store, cookie := libraryHTTP(t)
	ctx := context.Background()
	if _, _, err := store.Enqueue(ctx, "admin", "316664dc-7106-5acd-8df7-88c9d0d9baef", testkit.ScanPlan("316664dc-7106-5acd-8df7-88c9d0d9baef"), "failed-job-key"); err != nil {
		t.Fatal(err)
	}
	job, err := store.Claim(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Finish(ctx, job, 1, errors.New("private root path")); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/api/v1/scan-jobs/"+job.ID, nil)
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	server.ServeHTTP(w, r)
	testkit.ValidateHTTP(t, "ScanJob", w.Body.Bytes())
	if strings.Contains(w.Body.String(), "private") {
		t.Fatal("job error leaked raw storage detail")
	}
	if _, _, err := store.Enqueue(ctx, "other", "843558e1-a158-56ac-834a-baffd12bb5c5", testkit.ScanPlan("843558e1-a158-56ac-834a-baffd12bb5c5"), "private-job-key"); err != nil {
		t.Fatal(err)
	}
	job, err = store.Claim(ctx)
	if err != nil {
		t.Fatal(err)
	}
	r = httptest.NewRequest("GET", "/api/v1/scan-jobs/"+job.ID, nil)
	r.AddCookie(cookie)
	w = httptest.NewRecorder()
	server.ServeHTTP(w, r)
	if w.Code != 404 {
		t.Fatal("other user's job was disclosed")
	}
}
