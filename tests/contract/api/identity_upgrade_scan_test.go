package api_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/instanceid"
	"github.com/fnMrRice/MusicFS-server/src/jobs"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestIdentityUpgradeUnattemptedScanHTTPPreservesHistory(t *testing.T) {
	server, store, cookie := libraryHTTP(t)
	ctx := context.Background()
	plan := testkit.ScanPlan("music")
	plan.PluginID = "musicfs.localfiles"
	queued, _, err := store.Enqueue(ctx, "admin", "legacy-local", plan, "identity-upgrade-scan")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.MigratePluginInstanceIDs(ctx); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("GET", "/api/v1/scan-jobs/"+queued.ID, nil)
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	testkit.ValidateHTTP(t, "ScanJob", response.Body.Bytes())
	var job jobs.Job
	if err := json.Unmarshal(response.Body.Bytes(), &job); err != nil {
		t.Fatal(err)
	}
	if job.Status != "failed" || job.Error == nil || job.Error.Code != "RESOURCE_CHANGED" || job.Error.RequestID == "" || job.ProviderInstanceID != instanceid.Canonical("legacy-local") || job.AttemptCount != 0 || job.RetryCount != 0 || job.LastAttemptAt != nil || job.NextAttemptAt != nil {
		t.Fatalf("upgrade fabricated execution history or lost its explanation: %+v", job)
	}
	persisted, err := store.GetJob(ctx, "admin", queued.ID)
	if err != nil || persisted.PlanDigest != queued.PlanDigest || persisted.Plan.PluginID != plan.PluginID || persisted.AttemptCount != 0 {
		t.Fatalf("upgrade changed the captured execution ticket: %+v %v", persisted, err)
	}
	schema := testkit.ContractSchema(t, "http", "ScanJob")
	for _, code := range []string{"TEMPORARY_FAILURE", "INTERNAL"} {
		value := testkit.DecodeJSON(t, response.Body.Bytes()).(map[string]any)
		value["error"].(map[string]any)["code"] = code
		if err := schema.Validate(value); err == nil {
			t.Fatalf("unattempted failure compatibility was widened to %s", code)
		}
	}
	value := testkit.DecodeJSON(t, response.Body.Bytes()).(map[string]any)
	value["status"] = "running"
	if err := schema.Validate(value); err == nil {
		t.Fatal("running scan accepted without an execution attempt")
	}
}
