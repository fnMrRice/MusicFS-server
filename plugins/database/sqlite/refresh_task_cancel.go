package sqlite

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/tasks"
)

func (s *Store) CancelRefreshTask(ctx context.Context, owner, id string) (tasks.Task, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return tasks.Task{}, err
	}
	defer tx.Rollback()
	if strings.HasPrefix(id, operationPrefix) {
		return cancelQueuedOperation(ctx, tx, owner, id)
	}
	j, err := refreshJob(ctx, tx, owner, id)
	if err != nil {
		return tasks.Task{}, err
	}
	task, err := refreshTask(ctx, tx, j)
	if err != nil || !task.Cancellable {
		return task, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	var completed any
	// 排队和重试等待尚未占用执行资源；其他阶段等待宿主确认资源释放。
	if j.Status == "queued" || j.Status == "retry_wait" {
		completed = now
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO refresh_task_cancellations(job_id,requested_at,completed_at) VALUES(?,?,?)", id, now, completed); err != nil {
		return task, err
	}
	failure, _ := json.Marshal(domain.Error{Code: "CANCELED", Message: "Refresh task cancelled"})
	if _, err = tx.ExecContext(ctx, "UPDATE jobs SET state='cancelled',complete=0,error=?,next_attempt_at=NULL,updated_at=? WHERE id=? AND state IN ('queued','retry_wait','running')", string(failure), now, id); err != nil {
		return task, err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE provider_scan_leases SET active_job_id=NULL WHERE owner=? AND active_job_id=?", j.Owner, id); err != nil {
		return task, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE enrichment_jobs SET state='cancelled',updated_at=? WHERE owner=? AND id IN (SELECT job_id FROM metadata_automation WHERE owner=? AND scan_job_id=? AND state IN ('queued','preparing','running'))`, now, j.Owner, j.Owner, id); err != nil {
		return task, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE enrichment_targets SET state='cancelled',generation=generation+1,failure_code='CANCELED' WHERE owner=? AND state IN ('queued','running') AND job_id IN (SELECT job_id FROM metadata_automation WHERE owner=? AND scan_job_id=? AND state IN ('queued','preparing','running'))`, j.Owner, j.Owner, id); err != nil {
		return task, err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE metadata_automation SET state='failed',failure_code='CANCELED' WHERE owner=? AND scan_job_id=? AND state IN ('queued','preparing','running')", j.Owner, id); err != nil {
		return task, err
	}
	j, err = refreshJob(ctx, tx, owner, id)
	if err != nil {
		return task, err
	}
	task, err = refreshTask(ctx, tx, j)
	if err != nil {
		return task, err
	}
	return task, tx.Commit()
}

func (s *Store) RefreshTaskMetadataJobs(ctx context.Context, owner, id string) ([]string, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	j, err := refreshJob(ctx, tx, owner, id)
	if err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT DISTINCT job_id FROM metadata_automation WHERE owner=? AND scan_job_id=? AND job_id IS NOT NULL ORDER BY job_id", j.Owner, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *Store) CompleteRefreshTaskCancellation(ctx context.Context, owner, id string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = refreshJob(ctx, tx, owner, id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE metadata_automation_actions SET document=json_set(document,'$.status','skipped','$.reason','CANCELED') WHERE json_extract(document,'$.status')='pending' AND trigger_id IN (SELECT a.id FROM metadata_automation a JOIN refresh_task_cancellations c ON c.job_id=a.scan_job_id WHERE c.job_id=? AND c.completed_at IS NULL)`, id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE refresh_task_cancellations SET completed_at=? WHERE job_id=? AND completed_at IS NULL", time.Now().UTC().Format(time.RFC3339Nano), id); err != nil {
		return err
	}
	return tx.Commit()
}
