package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/enrichment"
)

func readEnrichmentJob(ctx context.Context, tx *sql.Tx, owner, id string) (enrichment.Job, error) {
	j := enrichment.Job{JobID: id, Targets: []enrichment.JobTarget{}}
	var request []byte
	var created, updated string
	err := tx.QueryRowContext(ctx, "SELECT state,request,created_at,updated_at FROM enrichment_jobs WHERE owner=? AND id=?", owner, id).Scan(&j.State, &request, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return j, domain.Fail("NOT_FOUND", "Metadata job not found")
	}
	if err != nil {
		return j, err
	}
	if err = json.Unmarshal(request, &j.Request); err != nil {
		return j, err
	}
	if j.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
		return j, err
	}
	if j.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated); err != nil {
		return j, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT target,state,issues,failure_code FROM enrichment_targets WHERE owner=? AND job_id=? ORDER BY source_id", owner, id)
	if err != nil {
		return j, err
	}
	for rows.Next() {
		t := enrichment.JobTarget{Candidates: []enrichment.PreviewCandidate{}}
		var target, issues []byte
		if err = rows.Scan(&target, &t.State, &issues, &t.FailureCode); err == nil {
			err = json.Unmarshal(target, &t.Target)
		}
		if err == nil {
			err = json.Unmarshal(issues, &t.Issues)
		}
		if err != nil {
			rows.Close()
			return j, err
		}
		j.Targets = append(j.Targets, t)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return j, err
	}
	for i := range j.Targets {
		rows, err = tx.QueryContext(ctx, "SELECT document FROM enrichment_candidates WHERE owner=? AND job_id=? AND source_id=? ORDER BY id", owner, id, j.Targets[i].Target.SourceTrackID)
		if err != nil {
			return j, err
		}
		for rows.Next() {
			var raw []byte
			var c enrichment.PreviewCandidate
			if err = rows.Scan(&raw); err == nil {
				err = json.Unmarshal(raw, &c)
			}
			if err != nil {
				rows.Close()
				return j, err
			}
			j.Targets[i].Candidates = append(j.Targets[i].Candidates, c)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return j, err
		}
	}
	return j, nil
}

func (s *Store) GetEnrichmentJob(ctx context.Context, owner, id string) (enrichment.Job, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return enrichment.Job{}, err
	}
	defer tx.Rollback()
	return readEnrichmentJob(ctx, tx, owner, id)
}

func readEnrichmentCandidate(ctx context.Context, tx *sql.Tx, owner, id string) (enrichment.PreviewCandidate, error) {
	var c enrichment.PreviewCandidate
	var raw []byte
	err := tx.QueryRowContext(ctx, "SELECT document FROM enrichment_candidates WHERE owner=? AND id=?", owner, id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return c, domain.Fail("NOT_FOUND", "Metadata candidate not found")
	}
	if err == nil {
		err = json.Unmarshal(raw, &c)
	}
	return c, err
}

// Retrieval does not authorize application; expiry/task state are checked in
// the selection transaction after replay detection, so old successes can replay.
func (s *Store) EnrichmentCandidate(ctx context.Context, owner, id string) (enrichment.PreviewCandidate, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return enrichment.PreviewCandidate{}, err
	}
	defer tx.Rollback()
	return readEnrichmentCandidate(ctx, tx, owner, id)
}

func executionEnrichment(ctx context.Context, tx *sql.Tx, w enrichment.Work) error {
	var jobState, targetState string
	var generation int64
	var raw, request []byte
	err := tx.QueryRowContext(ctx, `SELECT j.state,t.state,t.generation,t.target,j.request FROM enrichment_jobs j JOIN enrichment_targets t ON t.job_id=j.id AND t.owner=j.owner WHERE j.owner=? AND j.id=? AND t.source_id=?`, w.Owner, w.JobID, w.Target.SourceTrackID).Scan(&jobState, &targetState, &generation, &raw, &request)
	if err != nil {
		return err
	}
	var target enrichment.Target
	if err = json.Unmarshal(raw, &target); err != nil {
		return err
	}
	given, err := json.Marshal(w.Request)
	if err != nil {
		return err
	}
	if jobState != "running" || targetState != "running" || generation != w.Generation || target != w.Target || !bytes.Equal(request, given) {
		return domain.Fail("CONFLICT", "Metadata execution is no longer active")
	}
	return nil
}

func activeEnrichment(ctx context.Context, tx *sql.Tx, w enrichment.Work) error {
	if err := executionEnrichment(ctx, tx, w); err != nil {
		return err
	}
	return contentCurrentTarget(ctx, tx, w.Owner, enrichment.ContentTarget(w.Target))
}

func (s *Store) CurrentEnrichment(ctx context.Context, w enrichment.Work) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	return activeEnrichment(ctx, tx, w)
}
