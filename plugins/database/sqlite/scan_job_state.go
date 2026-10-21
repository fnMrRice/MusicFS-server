package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/jobs"
)

func (s *Store) Claim(ctx context.Context) (jobs.Job, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return jobs.Job{}, err
	}
	defer tx.Rollback()
	now := time.Now().UTC()
	j, err := readJob(tx.QueryRowContext(ctx, "SELECT "+jobColumns+" FROM jobs WHERE (state='queued' OR (state='retry_wait' AND julianday(next_attempt_at)<=julianday(?))) AND attempt_count<max_attempts AND NOT EXISTS(SELECT 1 FROM provider_scan_leases l WHERE l.owner=jobs.owner AND l.provider=jobs.target AND l.active_job_id IS NOT NULL) ORDER BY created_at,id LIMIT 1", now.Format(time.RFC3339Nano)))
	if err != nil {
		return j, err
	}
	// SQLite date comparison is rounded; never execute before the exact stored
	// deadline even when the coarse query includes the next fraction of a ms.
	if j.NextAttemptAt != nil && now.Before(*j.NextAttemptAt) {
		return jobs.Job{}, sql.ErrNoRows
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO provider_scan_leases(owner,provider,last_generation,active_job_id) VALUES(?,?,1,?) ON CONFLICT(owner,provider) DO UPDATE SET last_generation=last_generation+1,active_job_id=excluded.active_job_id WHERE active_job_id IS NULL", j.Owner, j.ProviderInstanceID, j.ID)
	if err != nil {
		return j, err
	}
	if err = tx.QueryRowContext(ctx, "SELECT last_generation FROM provider_scan_leases WHERE owner=? AND provider=? AND active_job_id=?", j.Owner, j.ProviderInstanceID, j.ID).Scan(&j.Generation); err != nil {
		return j, err
	}
	_, err = tx.ExecContext(ctx, "UPDATE jobs SET state='running',generation=?,attempt_count=attempt_count+1,next_attempt_at=NULL,last_attempt_at=?,observed=0,updated_at=? WHERE id=? AND owner=? AND state IN ('queued','retry_wait')", j.Generation, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), j.ID, j.Owner)
	if err != nil {
		return j, err
	}
	j.Status = "running"
	j.AttemptCount++
	j.RetryCount = j.AttemptCount - 1
	j.NextAttemptAt, j.LastAttemptAt, j.UpdatedAt = nil, &now, now
	j.Progress.Observed = 0
	return j, tx.Commit()
}

func activeScan(ctx context.Context, tx *sql.Tx, given jobs.Job) (jobs.Job, error) {
	j, err := readJob(tx.QueryRowContext(ctx, "SELECT "+jobColumns+" FROM jobs WHERE id=? AND owner=?", given.ID, given.Owner))
	if errors.Is(err, sql.ErrNoRows) {
		return j, domain.Fail("CONFLICT", "Scan task is no longer active")
	}
	if err != nil {
		return j, err
	}
	if j.Status != "running" || j.ProviderInstanceID != given.ProviderInstanceID || j.Generation != given.Generation || j.PlanDigest != given.PlanDigest || !reflect.DeepEqual(j.Plan, given.Plan) {
		return j, domain.Fail("CONFLICT", "Scan task plan or generation is no longer active")
	}
	var active int
	err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM provider_scan_leases WHERE owner=? AND provider=? AND active_job_id=? AND last_generation=?", j.Owner, j.ProviderInstanceID, j.ID, j.Generation).Scan(&active)
	if err != nil {
		return j, err
	}
	if active != 1 {
		return j, domain.Fail("CONFLICT", "Scan execution lease is no longer active")
	}
	return j, nil
}

func (s *Store) Finish(ctx context.Context, given jobs.Job, n int, failure error) error {
	if n < 0 {
		return domain.Fail("INVALID_ARGUMENT", "Invalid observed count")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	j, err := activeScan(ctx, tx, given)
	if err != nil {
		return err
	}
	var message, last, next any
	complete := failure == nil
	now := time.Now().UTC()
	decision := jobs.DecideRetry(failure, j.AttemptCount, now)
	if decision.Error != nil {
		encoded, err := json.Marshal(decision.Error)
		if err != nil {
			return err
		}
		message = string(encoded)
	} else {
		last = now.Format(time.RFC3339Nano)
	}
	if decision.NextAttemptAt != nil {
		next = decision.NextAttemptAt.Format(time.RFC3339Nano)
	}
	_, err = tx.ExecContext(ctx, "UPDATE jobs SET state=?,observed=?,complete=?,error=?,next_attempt_at=?,last_success_at=COALESCE(?,last_success_at),updated_at=? WHERE id=? AND owner=?", decision.Status, n, complete, message, next, last, now.Format(time.RFC3339Nano), j.ID, j.Owner)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE provider_scan_leases SET active_job_id=NULL WHERE owner=? AND provider=? AND active_job_id=? AND last_generation=?", j.Owner, j.ProviderInstanceID, j.ID, j.Generation); err != nil {
		return err
	}
	return tx.Commit()
}
