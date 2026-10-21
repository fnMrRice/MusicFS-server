package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/jobs"
)

const jobColumns = "id,owner,target,kind,generation,state,observed,complete,error,COALESCE(last_success_at,(SELECT history.last_success_at FROM jobs history WHERE history.owner=jobs.owner AND history.target=jobs.target AND history.plan_digest=jobs.plan_digest AND history.state='succeeded' AND history.last_success_at IS NOT NULL ORDER BY julianday(history.last_success_at) DESC LIMIT 1)),plan,plan_digest,attempt_count,max_attempts,next_attempt_at,last_attempt_at,created_at,updated_at"

type rowScanner interface{ Scan(...any) error }

func readJob(row rowScanner) (jobs.Job, error) {
	var j jobs.Job
	var failure, last, nextAttempt, lastAttempt sql.NullString
	var created, updated string
	var raw []byte
	if err := row.Scan(&j.ID, &j.Owner, &j.ProviderInstanceID, &j.Kind, &j.Generation, &j.Status, &j.Progress.Observed, &j.Complete, &failure, &last, &raw, &j.PlanDigest, &j.AttemptCount, &j.MaxAttempts, &nextAttempt, &lastAttempt, &created, &updated); err != nil {
		return j, err
	}
	if err := readJobTimes(&j, created, updated, nextAttempt, lastAttempt); err != nil {
		return j, err
	}
	if err := json.Unmarshal(raw, &j.Plan); err != nil {
		return j, err
	}
	canonical, err := j.Plan.Canonical()
	if err != nil {
		return j, err
	}
	j.Plan = canonical
	digest, err := j.Plan.Digest()
	if err != nil {
		return j, err
	}
	if digest != j.PlanDigest || j.Kind != "scan-"+j.Plan.Mode {
		return j, errors.New("persisted scan plan is invalid")
	}
	j.RootIDs = make([]string, 0, len(j.Plan.Roots))
	for _, root := range j.Plan.Roots {
		j.RootIDs = append(j.RootIDs, root.RootID)
	}
	j.EffectiveOptions, j.SchemaRevision = j.Plan.Options, j.Plan.SchemaRevision
	j.EffectiveRootOptions = j.Plan.RootOptions
	if failure.Valid {
		var detail domain.Error
		if json.Unmarshal([]byte(failure.String), &detail) != nil || detail.Code == "" || detail.Message == "" {
			detail = domain.Error{Code: "TEMPORARY_FAILURE", Message: "Scan failed; previous library retained"}
		}
		j.Error = &detail
	}
	if last.Valid {
		v, e := time.Parse(time.RFC3339Nano, last.String)
		if e != nil {
			return j, e
		}
		j.LastSuccessAt = &v
	}
	return j, nil
}

func (s *Store) Enqueue(ctx context.Context, owner, target string, plan pluginproto.ScanPlan, key string) (jobs.Job, bool, error) {
	var j jobs.Job
	if owner == "" || target == "" || len(key) < 8 || len(key) > 128 {
		return j, false, domain.Fail("INVALID_ARGUMENT", "Invalid scan task or idempotency key")
	}
	plan, err := plan.Canonical()
	if err != nil {
		return j, false, err
	}
	digest, err := plan.Digest()
	if err != nil {
		return j, false, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return j, false, err
	}
	defer tx.Rollback()
	route := "scan/" + target
	var priorDigest, expiry string
	var prior []byte
	err = tx.QueryRowContext(ctx, "SELECT digest,result,expires_at FROM idempotency WHERE owner=? AND route=? AND key=?", owner, route, key).Scan(&priorDigest, &prior, &expiry)
	if err == nil {
		if priorDigest != digest {
			return j, false, domain.Fail("CONFLICT", "Idempotency key was used with another scan plan")
		}
		until, e := time.Parse(time.RFC3339Nano, expiry)
		if e != nil || time.Now().After(until) {
			return j, false, domain.Fail("IDEMPOTENCY_EXPIRED", "Create a new operation")
		}
		j, err = readJob(tx.QueryRowContext(ctx, "SELECT "+jobColumns+" FROM jobs WHERE id=? AND owner=?", string(prior), owner))
		return j, true, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return j, false, err
	}
	j, err = readJob(tx.QueryRowContext(ctx, "SELECT "+jobColumns+" FROM jobs WHERE owner=? AND target=? AND plan_digest=? AND state IN ('queued','running','retry_wait') LIMIT 1", owner, target, digest))
	merged := err == nil
	if errors.Is(err, sql.ErrNoRows) {
		var count int
		if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM jobs WHERE state IN ('queued','running','retry_wait')").Scan(&count); err != nil {
			return j, false, err
		}
		if count >= 1024 {
			return j, false, domain.Fail("RATE_LIMITED", "Scan queue is full")
		}
		raw, err := json.Marshal(plan)
		if err != nil {
			return j, false, err
		}
		id := domain.NewID()
		now := time.Now().UTC().Format(time.RFC3339Nano)
		_, err = tx.ExecContext(ctx, "INSERT INTO jobs(id,owner,target,kind,generation,state,created_at,updated_at,plan,plan_digest) VALUES(?,?,?,?,0,'queued',?,?,?,?)", id, owner, target, "scan-"+plan.Mode, now, now, raw, digest)
		if err != nil {
			return j, false, err
		}
		j, err = readJob(tx.QueryRowContext(ctx, "SELECT "+jobColumns+" FROM jobs WHERE id=?", id))
	}
	if err != nil {
		return j, false, err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO idempotency(owner,route,key,digest,result,expires_at) VALUES(?,?,?,?,?,?)", owner, route, key, digest, []byte(j.ID), time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano))
	if err != nil {
		return j, false, err
	}
	return j, merged, tx.Commit()
}

func (s *Store) GetJob(ctx context.Context, owner, id string) (jobs.Job, error) {
	j, err := readJob(s.DB.QueryRowContext(ctx, "SELECT "+jobColumns+" FROM jobs WHERE id=? AND owner=?", id, owner))
	if errors.Is(err, sql.ErrNoRows) {
		err = domain.Fail("NOT_FOUND", "Job not found")
	}
	return j, err
}
