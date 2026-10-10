package sqlite

import (
	"context"
	"encoding/json"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/jobs"
)

// RecoverJobs invalidates interrupted execution generations. A crash consumes
// an attempt just like an explicitly retryable failure; restart never resets it.
func (s *Store) RecoverJobs(ctx context.Context) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, "SELECT "+jobColumns+" FROM jobs WHERE state='running'")
	if err != nil {
		return err
	}
	var interrupted []jobs.Job
	for rows.Next() {
		j, e := readJob(rows)
		if e != nil {
			rows.Close()
			return e
		}
		interrupted = append(interrupted, j)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	for _, j := range interrupted {
		d := jobs.DecideRetry(jobs.Retryable(domain.Fail("TEMPORARY_FAILURE", "Scan interrupted; previous library retained")), j.AttemptCount, now)
		raw, err := json.Marshal(d.Error)
		if err != nil {
			return err
		}
		var next any
		if d.NextAttemptAt != nil {
			next = d.NextAttemptAt.Format(time.RFC3339Nano)
		}
		if _, err = tx.ExecContext(ctx, "UPDATE jobs SET state=?,generation=0,error=?,next_attempt_at=?,updated_at=? WHERE id=? AND owner=?", d.Status, string(raw), next, now.Format(time.RFC3339Nano), j.ID, j.Owner); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, "UPDATE provider_scan_leases SET active_job_id=NULL"); err != nil {
		return err
	}
	return tx.Commit()
}
