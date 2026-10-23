package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/jobs"
	"github.com/fnMrRice/MusicFS-server/src/tasks"
)

func refreshJob(ctx context.Context, tx *sql.Tx, owner, id string) (jobs.Job, error) {
	j, err := readJob(tx.QueryRowContext(ctx, "SELECT "+jobColumns+" FROM jobs WHERE id=? AND (?='' OR owner=?) AND json_extract(plan,'$.refreshMetadata')=1", id, owner, owner))
	if errors.Is(err, sql.ErrNoRows) {
		err = domain.Fail("NOT_FOUND", "Refresh task not found")
	}
	return j, err
}

func refreshTask(ctx context.Context, tx *sql.Tx, j jobs.Job) (tasks.Task, error) {
	task := tasks.Task{ID: j.ID, Kind: "metadataRefresh", OwnerID: j.Owner, ProviderInstanceID: j.ProviderInstanceID, RootIDs: j.RootIDs, Roots: []tasks.Root{}, Status: "queued", Phase: "queued", Cancellable: true, CreatedAt: j.CreatedAt, UpdatedAt: j.UpdatedAt, Error: j.Error}
	task.Progress.Observed = j.Progress.Observed
	if err := refreshTaskSummary(ctx, tx, j, &task); err != nil {
		return task, err
	}
	switch j.Status {
	case "running":
		task.Status, task.Phase = "running", "scanning"
	case "succeeded":
		task.Status, task.Phase = "running", "metadata"
		if task.Progress.MetadataCompleted == task.Progress.MetadataTotal {
			task.Status, task.Phase, task.Cancellable = "succeeded", "finished", false
			if task.Result.Failed > 0 || task.Error != nil {
				task.Status = "failed"
			}
		}
	case "failed", "paused":
		task.Status, task.Phase, task.Cancellable = "failed", "finished", false
	case "cancelled":
		task.Status, task.Phase, task.Cancellable = "cancelled", "finished", false
	}
	var requested string
	var completed sql.NullString
	err := tx.QueryRowContext(ctx, "SELECT requested_at,completed_at FROM refresh_task_cancellations WHERE job_id=?", j.ID).Scan(&requested, &completed)
	if err == nil {
		task.CancelRequested, task.Cancellable, task.Status, task.Phase = true, false, "cancelling", "finished"
		stamp := requested
		if completed.Valid {
			task.Status, stamp = "cancelled", completed.String
		}
		if updated, e := time.Parse(time.RFC3339Nano, stamp); e == nil && updated.After(task.UpdatedAt) {
			task.UpdatedAt = updated
		}
		task.Error = &domain.Error{Code: "CANCELED", Message: "Refresh task cancelled"}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return task, err
	}
	return task, nil
}

func (s *Store) GetRefreshTask(ctx context.Context, owner, id string) (tasks.Task, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return tasks.Task{}, err
	}
	defer tx.Rollback()
	j, err := refreshJob(ctx, tx, owner, id)
	if err != nil {
		return tasks.Task{}, err
	}
	return refreshTask(ctx, tx, j)
}

func (s *Store) ListRefreshTasks(ctx context.Context, owner, cursor string, limit int) (tasks.Page, error) {
	page := tasks.Page{Items: []tasks.Task{}}
	if limit < 1 || limit > 200 {
		return page, domain.Fail("INVALID_ARGUMENT", "Invalid task page size")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return page, err
	}
	defer tx.Rollback()
	var cursorTime string
	if cursor != "" {
		j, err := refreshJob(ctx, tx, owner, cursor)
		if err != nil {
			return page, domain.Fail("INVALID_ARGUMENT", "Invalid task cursor")
		}
		cursorTime = j.CreatedAt.Format(time.RFC3339Nano)
	}
	rows, err := tx.QueryContext(ctx, `SELECT id FROM jobs WHERE json_extract(plan,'$.refreshMetadata')=1 AND (?='' OR owner=?) AND (?='' OR rtrim(created_at,'Z')<rtrim(?,'Z') OR (created_at=? AND id<?)) ORDER BY rtrim(created_at,'Z') DESC,id DESC LIMIT ?`, owner, owner, cursor, cursorTime, cursorTime, cursor, limit+1)
	if err != nil {
		return page, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return page, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return page, err
	}
	if len(ids) > limit {
		id := ids[limit-1]
		page.NextCursor = &id
		ids = ids[:limit]
	}
	for _, id := range ids {
		j, err := refreshJob(ctx, tx, owner, id)
		if err != nil {
			return page, err
		}
		task, err := refreshTask(ctx, tx, j)
		if err != nil {
			return page, err
		}
		page.Items = append(page.Items, task)
	}
	err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM jobs j LEFT JOIN refresh_task_cancellations c ON c.job_id=j.id
 WHERE json_extract(j.plan,'$.refreshMetadata')=1 AND (?='' OR j.owner=?) AND
 ((c.job_id IS NOT NULL AND c.completed_at IS NULL) OR (c.job_id IS NULL AND
 (j.state IN ('queued','retry_wait','running') OR (j.state='succeeded' AND EXISTS(SELECT 1 FROM metadata_automation a WHERE a.scan_job_id=j.id AND a.owner=j.owner AND a.scan_generation=j.generation AND a.plan_digest=j.plan_digest AND a.reason='refreshMetadata' AND a.state IN ('queued','preparing','running'))))))`, owner, owner).Scan(&page.ActiveCount)
	return page, err
}

var _ tasks.Repository = (*Store)(nil)
