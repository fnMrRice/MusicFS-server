package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/instanceid"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/metadata/enrichment"
	"github.com/fnMrRice/MusicFS-server/src/metadata/writebackhost"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginid"
)

const identityMigrationMessage = "Plugin instance identity changed during upgrade; submit a new request"

type migrationJob struct {
	id, state string
	raw       []byte
}

func migrationJobs(ctx context.Context, tx *sql.Tx, query string) ([]migrationJob, error) {
	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []migrationJob{}
	for rows.Next() {
		var item migrationJob
		if err := rows.Scan(&item.id, &item.raw, &item.state); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func migrateInstanceJobs(ctx context.Context, tx *sql.Tx) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	failure := &domain.Error{Code: "RESOURCE_CHANGED", Message: identityMigrationMessage}
	rawFailure, _ := json.Marshal(failure)
	items, err := migrationJobs(ctx, tx, "SELECT id,work,state FROM writeback_jobs WHERE state IN ('queued','running','recovery')")
	if err != nil {
		return err
	}
	for _, item := range items {
		var work writebackhost.Work
		if err := json.Unmarshal(item.raw, &work); err != nil {
			return err
		}
		if instanceid.Canonical(work.Target.InstanceID) == work.Target.InstanceID {
			continue
		}
		if item.state != "queued" {
			return domain.Fail("CONFLICT", "Legacy plugin instances have unfinished file writes; finish writeback recovery with the previous server before upgrading")
		}
		if _, err := tx.ExecContext(ctx, "UPDATE writeback_jobs SET state='failed',error=?,updated_at=? WHERE id=?", rawFailure, now, item.id); err != nil {
			return err
		}
	}
	items, err = migrationJobs(ctx, tx, "SELECT id,plan,target FROM jobs WHERE state IN ('queued','running','retry_wait')")
	if err != nil {
		return err
	}
	for _, item := range items {
		var plan pluginproto.ScanPlan
		if err := json.Unmarshal(item.raw, &plan); err != nil {
			return err
		}
		if instanceid.Canonical(item.state) == item.state && pluginid.Canonical(plan.PluginID) == plan.PluginID {
			continue
		}
		if _, err := tx.ExecContext(ctx, "UPDATE jobs SET state='failed',generation=0,error=?,next_attempt_at=NULL,updated_at=? WHERE id=?", string(rawFailure), now, item.id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE provider_scan_leases SET active_job_id=NULL WHERE active_job_id=?", item.id); err != nil {
			return err
		}
	}
	items, err = migrationJobs(ctx, tx, "SELECT id,document,state FROM remote_sync_jobs WHERE state IN ('queued','running')")
	if err != nil {
		return err
	}
	for _, item := range items {
		var record remoteJobRecord
		if err := json.Unmarshal(item.raw, &record); err != nil {
			return err
		}
		if instanceid.Canonical(record.Binding.InstanceID) == record.Binding.InstanceID {
			continue
		}
		record.Job.State, record.Job.Error = "failed", failure
		record.Job.UpdatedAt = time.Now().UTC()
		raw, err := json.Marshal(record)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE remote_sync_jobs SET state='failed',document=? WHERE id=?", raw, item.id); err != nil {
			return err
		}
	}
	if err := failLegacyEnrichment(ctx, tx, now); err != nil {
		return err
	}
	return failLegacyAutomation(ctx, tx)
}

func changedInstances(ids []string) bool {
	for _, id := range ids {
		if instanceid.Canonical(id) != id {
			return true
		}
	}
	return false
}

func failLegacyEnrichment(ctx context.Context, tx *sql.Tx, now string) error {
	items, err := migrationJobs(ctx, tx, "SELECT id,request,state FROM enrichment_jobs WHERE state IN ('queued','running')")
	if err != nil {
		return err
	}
	for _, item := range items {
		var request enrichment.JobRequest
		if err := json.Unmarshal(item.raw, &request); err != nil {
			return err
		}
		if !changedInstances(request.InstanceIDs) && !changedInstances(request.ExcludeInstanceIDs) {
			continue
		}
		if _, err := tx.ExecContext(ctx, "UPDATE enrichment_jobs SET state='failed',updated_at=? WHERE id=?", now, item.id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE enrichment_targets SET state='failed',generation=generation+1,failure_code='RESOURCE_CHANGED' WHERE job_id=? AND state IN ('queued','running')", item.id); err != nil {
			return err
		}
	}
	return nil
}

func failLegacyAutomation(ctx context.Context, tx *sql.Tx) error {
	items, err := migrationJobs(ctx, tx, "SELECT id,COALESCE(policy,'null'),state FROM metadata_automation WHERE state IN ('queued','preparing','running')")
	if err != nil {
		return err
	}
	for _, item := range items {
		var policy enrichment.AutomaticPolicy
		if err := json.Unmarshal(item.raw, &policy); err != nil {
			return err
		}
		if changedInstances(policy.InstancePriority) {
			if _, err := tx.ExecContext(ctx, "UPDATE metadata_automation SET state='failed',failure_code='RESOURCE_CHANGED' WHERE id=?", item.id); err != nil {
				return err
			}
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE metadata_automation SET state='failed',failure_code='RESOURCE_CHANGED'
		WHERE state IN ('queued','preparing','running') AND scan_job_id IN (
			SELECT id FROM jobs WHERE json_valid(error) AND json_extract(error,'$.message')=?)`, identityMigrationMessage)
	return err
}
