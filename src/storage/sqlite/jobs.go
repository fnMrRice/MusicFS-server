package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/jobs"
	"time"
)

const jobColumns = "id,owner,target,kind,generation,state,observed,complete,error,COALESCE(last_success_at,(SELECT history.last_success_at FROM jobs history WHERE history.owner=jobs.owner AND history.target=jobs.target AND history.state='succeeded' AND history.last_success_at IS NOT NULL ORDER BY julianday(history.last_success_at) DESC LIMIT 1))"

type rowScanner interface{ Scan(...any) error }

func readJob(row rowScanner) (jobs.Job, error) {
	var j jobs.Job
	var failure, last sql.NullString
	err := row.Scan(&j.ID, &j.Owner, &j.ProviderInstanceID, &j.Kind, &j.Generation, &j.Status, &j.Progress.Observed, &j.Complete, &failure, &last)
	if failure.Valid {
		var detail domain.Error
		if json.Unmarshal([]byte(failure.String), &detail) != nil || detail.Code == "" || detail.Message == "" {
			detail = domain.Error{Code: "TEMPORARY_FAILURE", Message: "Scan failed; previous library retained"}
		}
		j.Error = &detail
	}
	if last.Valid {
		v, _ := time.Parse(time.RFC3339Nano, last.String)
		j.LastSuccessAt = &v
	}
	return j, err
}
func (s *Store) Enqueue(ctx context.Context, user, target, kind, key string) (jobs.Job, bool, error) {
	if len(key) < 8 || len(key) > 128 {
		return jobs.Job{}, false, domain.Fail("INVALID_ARGUMENT", "Idempotency-Key must contain 8 to 128 characters")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return jobs.Job{}, false, err
	}
	defer tx.Rollback()
	route := "scan/" + target
	digest := kind
	var priorDigest, expiry string
	var prior []byte
	err = tx.QueryRowContext(ctx, "SELECT digest,result,expires_at FROM idempotency WHERE owner=? AND route=? AND key=?", user, route, key).Scan(&priorDigest, &prior, &expiry)
	if err == nil {
		if priorDigest != digest {
			return jobs.Job{}, false, domain.Fail("CONFLICT", "Idempotency key was used with another request")
		}
		if expiry < time.Now().UTC().Format(time.RFC3339Nano) {
			return jobs.Job{}, false, domain.Fail("IDEMPOTENCY_EXPIRED", "Create a new operation")
		}
		var old jobs.Job
		json.Unmarshal(prior, &old)
		j, e := readJob(tx.QueryRowContext(ctx, "SELECT "+jobColumns+" FROM jobs WHERE id=? AND owner=?", old.ID, user))
		return j, true, e
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return jobs.Job{}, false, err
	}
	j, err := readJob(tx.QueryRowContext(ctx, "SELECT "+jobColumns+" FROM jobs WHERE owner=? AND target=? AND state IN ('queued','running') ORDER BY generation DESC LIMIT 1", user, target))
	merged := err == nil
	if errors.Is(err, sql.ErrNoRows) {
		j = jobs.Job{ID: domain.NewID(), Owner: user, ProviderInstanceID: target, Kind: kind, Status: "queued"}
		tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(generation),0)+1 FROM jobs WHERE target=?", target).Scan(&j.Generation)
		now := time.Now().UTC().Format(time.RFC3339Nano)
		_, err = tx.ExecContext(ctx, "INSERT INTO jobs(id,owner,target,kind,generation,state,created_at,updated_at) VALUES(?,?,?,?,?,'queued',?,?)", j.ID, user, target, kind, j.Generation, now, now)
	}
	if err != nil {
		return jobs.Job{}, false, err
	}
	result, _ := json.Marshal(j)
	_, err = tx.ExecContext(ctx, "INSERT INTO idempotency VALUES(?,?,?,?,?,?)", user, route, key, digest, result, time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano))
	if err != nil {
		return j, false, err
	}
	return j, merged, tx.Commit()
}
func (s *Store) Claim(ctx context.Context) (jobs.Job, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return jobs.Job{}, err
	}
	defer tx.Rollback()
	j, err := readJob(tx.QueryRowContext(ctx, "SELECT "+jobColumns+" FROM jobs WHERE state='queued' ORDER BY created_at LIMIT 1"))
	if err != nil {
		return j, err
	}
	_, err = tx.ExecContext(ctx, "UPDATE jobs SET state='running',updated_at=? WHERE id=? AND state='queued'", time.Now().UTC().Format(time.RFC3339Nano), j.ID)
	if err != nil {
		return j, err
	}
	j.Status = "running"
	return j, tx.Commit()
}
func (s *Store) Finish(ctx context.Context, j jobs.Job, n int, failure error) error {
	state := "succeeded"
	var message, last any
	complete := failure == nil
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if failure != nil {
		state = "failed"
		detail := domain.Error{Code: "TEMPORARY_FAILURE", Message: "Scan failed; previous library retained"}
		var known *domain.Error
		if errors.As(failure, &known) {
			detail = *known
		}
		if errors.Is(failure, context.Canceled) {
			state = "cancelled"
			detail.Code, detail.Message = "CANCELED", "Scan cancelled; previous library retained"
		}
		encoded, err := json.Marshal(detail)
		if err != nil {
			return err
		}
		message = string(encoded)
	} else {
		last = now
	}
	_, err := s.DB.ExecContext(ctx, "UPDATE jobs SET state=?,observed=?,complete=?,error=?,last_success_at=COALESCE(?,last_success_at),updated_at=? WHERE id=? AND generation=? AND state='running'", state, n, complete, message, last, now, j.ID, j.Generation)
	return err
}
func (s *Store) GetJob(ctx context.Context, user, id string) (jobs.Job, error) {
	j, err := readJob(s.DB.QueryRowContext(ctx, "SELECT "+jobColumns+" FROM jobs WHERE id=? AND owner=?", id, user))
	if errors.Is(err, sql.ErrNoRows) {
		err = domain.Fail("NOT_FOUND", "Job not found")
	}
	return j, err
}
func (s *Store) RecoverJobs(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, "UPDATE jobs SET state='queued',generation=generation+1 WHERE state='running'")
	return err
}
