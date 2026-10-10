package api_test

import (
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/identification"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestHashJobAndConflictDTOsMatchContract(t *testing.T) {
	now := time.Date(2026, 10, 10, 1, 0, 0, 0, time.UTC)
	job := identification.HashJob{ID: "hash-job", HashTarget: identification.HashTarget{SourceTrackID: "track", AssetID: "asset", AssetVersion: "version", FileSizeBytes: 4},
		Progress: identification.HashProgress{TotalBytes: 4}, CreatedAt: now, UpdatedAt: now}
	for _, state := range []string{"queued", "running", "succeeded", "failed", "unavailable", "cancelled", "superseded"} {
		job.Status = state
		job.Error = nil
		if state == "failed" || state == "unavailable" || state == "cancelled" || state == "superseded" {
			job.Error = &domain.Error{Code: "RESOURCE_CHANGED", Message: "Hash target changed", RequestID: "request-id"}
		}
		testkit.ValidateHTTP(t, "HashJob", evidenceJSON(t, job))
	}
	for _, mutate := range []func(map[string]any){
		func(v map[string]any) { delete(v, "assetVersion") },
		func(v map[string]any) { v["status"] = "verified" },
		func(v map[string]any) { v["progress"].(map[string]any)["completedBytes"] = -1 },
		func(v map[string]any) { v["progress"].(map[string]any)["totalBytes"] = nil },
		func(v map[string]any) { v["updatedAt"] = "2026-10-10T01:00:00+08:00" },
	} {
		value := testkit.DecodeJSON(t, evidenceJSON(t, job)).(map[string]any)
		mutate(value)
		if testkit.ContractSchema(t, "http", "HashJob").Validate(value) == nil {
			t.Fatal("invalid hash job contract accepted")
		}
	}
	conflict := identification.HashConflict{ID: "conflict", ProviderEvidenceID: "provider-evidence", VerifiedEvidenceID: "verified-evidence", Reason: "PROVIDER_SHA512_MISMATCH", DetectedAt: now}
	testkit.ValidateHTTP(t, "HashConflict", evidenceJSON(t, conflict))
	conflict.Reason = "MATCH"
	if testkit.ContractSchema(t, "http", "HashConflict").Validate(testkit.DecodeJSON(t, evidenceJSON(t, conflict))) == nil {
		t.Fatal("unsupported conflict reason accepted")
	}
}
