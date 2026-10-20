package sqlite_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/instanceid"
	"github.com/fnMrRice/MusicFS-server/src/enrichment"
	"github.com/fnMrRice/MusicFS-server/src/writeback"
	"github.com/fnMrRice/MusicFS-server/src/writebackhost"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestInstanceMigrationInvalidatesOldRequestsWithoutRewritingTickets(t *testing.T) {
	ctx := context.Background()
	store := migrationStore(t)
	legacyTrack(t, store, "track-one", "local")
	now := "2026-10-20T00:00:00Z"
	for _, item := range []struct{ id, instance string }{
		{"old-enrichment", "scraper-local"},
		{"current-enrichment", instanceid.Canonical("scraper-local")},
	} {
		request := enrichment.JobRequest{SourceTrackIDs: []string{"track-one"}, InstanceIDs: []string{item.instance}, Fields: []string{"lyrics"}}
		migrationSQL(t, store, "INSERT INTO enrichment_jobs VALUES(?, 'admin','queued',?,?,?)", item.id, migrationJSON(t, request), now, now)
		migrationSQL(t, store, "INSERT INTO enrichment_targets(job_id,source_id,owner,state,generation,target,issues) VALUES(?,'track-one','admin','queued',1,'{}','[]')", item.id)
	}
	work := writebackhost.Work{Job: writebackhost.Job{ID: "write-one"}, Target: writeback.Target{SourceTrackID: "track-one", InstanceID: "local"}}
	workRaw := migrationJSON(t, work)
	migrationSQL(t, store, "INSERT INTO writeback_jobs(id,owner,source_id,state,old_size,work,created_at,updated_at) VALUES('write-one','admin','track-one','queued',12,?,?,?)", workRaw, now, now)
	plan := testkit.ScanPlan("music")
	scan, _, err := store.Enqueue(ctx, "admin", "local", plan, "old-automation-scan")
	if err != nil {
		t.Fatal(err)
	}
	policyRaw := migrationJSON(t, enrichment.AutomaticPolicy{Enabled: true, InstancePriority: []string{"scraper-local"}, Fields: []string{"lyrics"}})
	migrationSQL(t, store, `INSERT INTO metadata_automation(id,owner,scan_job_id,scan_generation,plan_digest,root_id,source_id,asset_id,asset_version,reason,state,policy,created_at)
		VALUES('automation','admin',?,1,?,'music','track-one','asset-track-one','v1','automatic','queued',?,?)`, scan.ID, scan.PlanDigest, policyRaw, now)
	if err := store.MigratePluginInstanceIDs(ctx); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct{ id, expected, instance string }{
		{"old-enrichment", "failed", "scraper-local"},
		{"current-enrichment", "queued", instanceid.Canonical("scraper-local")},
	} {
		var state string
		var raw []byte
		if err := store.DB.QueryRow("SELECT state,request FROM enrichment_jobs WHERE id=?", item.id).Scan(&state, &raw); err != nil {
			t.Fatal(err)
		}
		var request enrichment.JobRequest
		if err := json.Unmarshal(raw, &request); err != nil || state != item.expected || len(request.InstanceIDs) != 1 || request.InstanceIDs[0] != item.instance {
			t.Fatalf("captured metadata request changed: state=%s request=%+v err=%v", state, request, err)
		}
	}
	var state, failure string
	var persisted []byte
	if err := store.DB.QueryRow("SELECT state,work,json_extract(error,'$.code') FROM writeback_jobs WHERE id='write-one'").Scan(&state, &persisted, &failure); err != nil || state != "failed" || failure != "RESOURCE_CHANGED" || string(persisted) != string(workRaw) {
		t.Fatalf("queued file-write ticket was rebound: state=%s code=%s err=%v", state, failure, err)
	}
	if err := store.DB.QueryRow("SELECT state,policy,failure_code FROM metadata_automation WHERE id='automation'").Scan(&state, &persisted, &failure); err != nil || state != "failed" || failure != "RESOURCE_CHANGED" || string(persisted) != string(policyRaw) {
		t.Fatalf("captured automatic policy was rebound: state=%s code=%s err=%v", state, failure, err)
	}
}
