package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/library/jobs"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestProviderScanStatusRestoresExistingJobWithoutSubmittingScan(t *testing.T) {
	const provider = "316664dc-7106-5acd-8df7-88c9d0d9baef"
	server, store, cookie := libraryHTTP(t)
	ctx := context.Background()
	read := func() jobs.ScanStatus {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, "/api/v1/provider-instances/"+provider+"/scan-status", nil)
		r.AddCookie(cookie)
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("scan status HTTP %d: %s", w.Code, w.Body.String())
		}
		testkit.ValidateHTTP(t, "ScanStatus", w.Body.Bytes())
		for _, private := range []string{"private", "planDigest", "rootIdentity", `"owner"`} {
			if strings.Contains(w.Body.String(), private) {
				t.Fatalf("scan status exposed internal data: %s", private)
			}
		}
		var status jobs.ScanStatus
		if err := json.Unmarshal(w.Body.Bytes(), &status); err != nil {
			t.Fatal(err)
		}
		return status
	}
	if status := read(); status.Job != nil {
		t.Fatal("empty provider did not return null")
	}
	queued, _, err := store.Enqueue(ctx, "admin", provider, testkit.ScanPlan(provider), "other-window-scan")
	if err != nil {
		t.Fatal(err)
	}
	if status := read(); status.Job == nil || status.Job.ID != queued.ID || status.Job.Status != "queued" {
		t.Fatalf("existing queued job missing: %+v", status.Job)
	}
	active, err := store.Claim(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateProgress(ctx, active, 200); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if status := read(); status.Job == nil || status.Job.ID != active.ID || status.Job.Status != "running" || status.Job.Progress.Observed != 200 {
			t.Fatalf("existing running job missing: %+v", status.Job)
		}
	}
	if err := store.Finish(ctx, active, 100, errors.New("private source path")); err != nil {
		t.Fatal(err)
	}
	if status := read(); status.Job == nil || status.Job.Status != "failed" || status.Job.Error == nil || status.Job.Error.RequestID == "" || status.Job.Progress.Observed != 200 {
		t.Fatalf("completed task and safe error missing: %+v", status.Job)
	}
	var count, attempts int
	if err := store.DB.QueryRowContext(ctx, "SELECT COUNT(*),SUM(attempt_count) FROM jobs").Scan(&count, &attempts); err != nil || count != 1 || attempts != 1 {
		t.Fatalf("GET submitted or claimed a scan: count=%d attempts=%d error=%v", count, attempts, err)
	}
	for _, test := range []struct {
		provider string
		cookie   *http.Cookie
		status   int
	}{{provider, nil, http.StatusUnauthorized}, {"843558e1-a158-56ac-834a-baffd12bb5c5", cookie, http.StatusNotFound}, {"missing", cookie, http.StatusNotFound}} {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/provider-instances/"+test.provider+"/scan-status", nil)
		if test.cookie != nil {
			r.AddCookie(test.cookie)
		}
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		if w.Code != test.status {
			t.Fatalf("unauthorized scan status HTTP %d: %s", w.Code, w.Body.String())
		}
		testkit.ValidateHTTP(t, "Error", w.Body.Bytes())
	}
}
