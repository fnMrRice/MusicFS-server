package api_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/jobs"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestScanRetryHTTPProjectionAndOriginalOperation(t *testing.T) {
	for _, code := range []string{"TEMPORARY_FAILURE", "AUTH_REQUIRED", "SOURCE_OFFLINE", "RESOURCE_CHANGED"} {
		t.Run(code, func(t *testing.T) {
			server, store, cookie := libraryHTTP(t)
			ctx := context.Background()
			plan := testkit.ScanPlan("mine")
			queued, _, err := store.Enqueue(ctx, "admin", "mine", plan, "http-retry-request")
			if err != nil {
				t.Fatal(err)
			}
			get := func() jobs.Job {
				t.Helper()
				r := httptest.NewRequest("GET", "/api/v1/scan-jobs/"+queued.ID, nil)
				r.AddCookie(cookie)
				w := httptest.NewRecorder()
				server.ServeHTTP(w, r)
				if w.Code != 200 {
					t.Fatal(w.Code, w.Body.String())
				}
				testkit.ValidateHTTP(t, "ScanJob", w.Body.Bytes())
				var value jobs.Job
				if err := json.Unmarshal(w.Body.Bytes(), &value); err != nil {
					t.Fatal(err)
				}
				return value
			}
			initial := get()
			if initial.AttemptCount != 0 || initial.NextAttemptAt != nil || initial.LastAttemptAt != nil {
				t.Fatal("new job projected as retry")
			}
			active, err := store.Claim(ctx)
			if err != nil {
				t.Fatal(err)
			}
			running := get()
			if running.Status != "running" || running.AttemptCount != 1 || running.MaxAttempts != 3 || running.LastAttemptAt == nil {
				t.Fatal("running budget missing")
			}
			if err = store.Finish(ctx, active, 3, jobs.Retryable(domain.Fail(code, "Provider requires attention"))); err != nil {
				t.Fatal(err)
			}
			current := get()
			want := "paused"
			if code == "TEMPORARY_FAILURE" {
				want = "retry_wait"
			}
			if code == "RESOURCE_CHANGED" {
				want = "failed"
			}
			if current.Status != want || current.Error == nil || current.Error.RequestID == "" || current.AttemptCount != 1 || current.RetryCount != 0 {
				t.Fatal("wrong public transition", current)
			}
			if store.ProviderScanState(ctx, "admin", "mine") != want {
				t.Fatal("provider state hid retry/pause")
			}
			persisted, err := store.GetJob(ctx, "admin", queued.ID)
			if err != nil {
				t.Fatal(err)
			}
			if persisted.Error.RequestID != "" {
				t.Fatal("HTTP request identity polluted persistent error")
			}
			replayed, merged, err := store.Enqueue(ctx, "admin", "mine", plan, "http-retry-request")
			if err != nil || !merged || !reflect.DeepEqual(replayed, persisted) {
				t.Fatal("original operation replenished retry budget", err)
			}
			if _, err = store.GetJob(ctx, "other", queued.ID); err == nil {
				t.Fatal("retry state leaked to another owner")
			}
		})
	}
}
