package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/metadata/enrichment"
)

func (s *Store) CancelEnrichment(ctx context.Context, owner, id string) (enrichment.Job, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return enrichment.Job{}, err
	}
	defer tx.Rollback()
	j, err := readEnrichmentJob(ctx, tx, owner, id)
	if err != nil || j.State == "cancelled" {
		return j, err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE enrichment_jobs SET state='cancelled',updated_at=? WHERE owner=? AND id=?", time.Now().UTC().Format(time.RFC3339Nano), owner, id); err != nil {
		return j, err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE enrichment_targets SET state='cancelled',generation=generation+1,failure_code='CANCELED' WHERE owner=? AND job_id=? AND state IN ('queued','running')", owner, id); err != nil {
		return j, err
	}
	j, err = readEnrichmentJob(ctx, tx, owner, id)
	if err != nil {
		return j, err
	}
	return j, tx.Commit()
}

func (s *Store) RecoverEnrichment(ctx context.Context) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `DELETE FROM enrichment_candidates WHERE EXISTS(SELECT 1 FROM enrichment_targets t WHERE t.job_id=enrichment_candidates.job_id AND t.source_id=enrichment_candidates.source_id AND t.state='running')`); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE enrichment_targets SET state='queued',generation=generation+1,issues=?,failure_code=NULL,preview_bytes=0,candidate_count=0 WHERE state='running'", []byte("[]")); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE enrichment_jobs SET state='queued',updated_at=? WHERE state='running'", time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) FinishEnrichment(ctx context.Context, w enrichment.Work, out enrichment.Outcome) error {
	if len(out.Candidates) > enrichment.MaxCandidates || len(out.Issues) > 128 || (out.FailureCode != nil && !enrichmentCode(*out.FailureCode)) {
		return domain.Fail("INVALID_ARGUMENT", "Invalid metadata outcome")
	}
	for _, issue := range out.Issues {
		if len(issue.InstanceID) > 128 || !enrichmentCode(issue.Code) {
			return domain.Fail("INVALID_ARGUMENT", "Invalid metadata issue")
		}
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = executionEnrichment(ctx, tx, w); err != nil {
		return err
	}
	if err = contentCurrentTarget(ctx, tx, w.Owner, enrichment.ContentTarget(w.Target)); err != nil {
		var known *domain.Error
		if !errors.As(err, &known) || (known.Code != "RESOURCE_CHANGED" && known.Code != "NOT_FOUND") {
			return err
		}
		code := "RESOURCE_CHANGED"
		out.FailureCode = &code
		out.Candidates = nil
	}
	var reserved int
	var reservedBytes, actualBytes int64
	if err = tx.QueryRowContext(ctx, "SELECT candidate_count,preview_bytes FROM enrichment_targets WHERE job_id=? AND source_id=?", w.JobID, w.Target.SourceTrackID).Scan(&reserved, &reservedBytes); err != nil {
		return err
	}
	if len(out.Candidates) > reserved {
		return domain.Fail("CONFLICT", "Metadata candidates exceed reserved budget")
	}
	for _, candidate := range out.Candidates {
		if err = validateEnrichmentCandidate(ctx, tx, w, candidate); err != nil {
			return err
		}
		actualBytes += candidate.SizeBytes
		if actualBytes > reservedBytes {
			return domain.Fail("CONFLICT", "Metadata preview bytes exceed reserved budget")
		}
		raw, err := json.Marshal(candidate)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO enrichment_candidates(id,owner,job_id,source_id,generation,object_id,expires_at,document) VALUES(?,?,?,?,?,?,?,?)", candidate.CandidateID, w.Owner, w.JobID, w.Target.SourceTrackID, w.Generation, candidate.PreviewObjectID, candidate.ExpiresAt.Format(time.RFC3339Nano), raw); err != nil {
			return err
		}
	}
	state := "completed"
	if out.FailureCode != nil {
		state = "failed"
	}
	if out.Issues == nil {
		out.Issues = []enrichment.JobIssue{}
	}
	issues, err := json.Marshal(out.Issues)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE enrichment_targets SET state=?,issues=?,failure_code=? WHERE job_id=? AND source_id=? AND generation=?", state, issues, out.FailureCode, w.JobID, w.Target.SourceTrackID, w.Generation); err != nil {
		return err
	}
	if err = finishEnrichmentJob(ctx, tx, w.JobID); err != nil {
		return err
	}
	return tx.Commit()
}

func finishEnrichmentJob(ctx context.Context, tx *sql.Tx, id string) error {
	var active, complete int
	err := tx.QueryRowContext(ctx, "SELECT SUM(CASE WHEN state IN ('queued','running') THEN 1 ELSE 0 END),SUM(CASE WHEN state='completed' THEN 1 ELSE 0 END) FROM enrichment_targets WHERE job_id=?", id).Scan(&active, &complete)
	if err != nil {
		return err
	}
	state := "running"
	if active == 0 {
		state = "failed"
		if complete > 0 {
			state = "completed"
		}
	}
	_, err = tx.ExecContext(ctx, "UPDATE enrichment_jobs SET state=?,updated_at=? WHERE id=? AND state<>'cancelled'", state, time.Now().UTC().Format(time.RFC3339Nano), id)
	return err
}

func enrichmentCode(code string) bool {
	if code == "" || len(code) > 64 {
		return false
	}
	for _, c := range code {
		if !(c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_') {
			return false
		}
	}
	return true
}

func validateEnrichmentCandidate(ctx context.Context, tx *sql.Tx, w enrichment.Work, c enrichment.PreviewCandidate) error {
	bad := func() error { return domain.Fail("INVALID_ARGUMENT", "Invalid metadata candidate") }
	if !enrichment.ValidRef(c.CandidateID) || c.Target != w.Target || len(c.InstanceID) < 1 || len(c.InstanceID) > 128 || (c.Field != "lyrics" && c.Field != "artwork") || len(c.Language) > 35 || len(c.Basis) > 64 || c.ExpiresAt.Before(time.Now()) || c.ExpiresAt.After(time.Now().Add(enrichment.CandidateLifetime+time.Second)) {
		return bad()
	}
	fromRequestedSource := slices.Contains(w.Request.InstanceIDs, c.InstanceID)
	if c.MatchedCatalogLyrics() {
		fromRequestedSource = w.DirectoryRefresh
	}
	if !fromRequestedSource || !slices.Contains(w.Request.Fields, c.Field) {
		return bad()
	}
	if !enrichment.ValidProvenance(c.Origin, c.Basis, c.Field) {
		return bad()
	}
	if c.Status == "unavailable" {
		if c.PreviewObjectID != nil || c.SizeBytes != 0 || c.SHA256 != "" || c.Format != "" || c.MIME != "" || c.FailureCode == nil || !enrichmentCode(*c.FailureCode) {
			return bad()
		}
		return nil
	}
	if c.Status != "available" || c.PreviewObjectID == nil || c.FailureCode != nil {
		return bad()
	}
	o, err := readContentObject(ctx, tx, w.Owner, *c.PreviewObjectID)
	if err != nil {
		return err
	}
	if o.State != "available" || o.Metadata.Target != enrichment.ContentTarget(w.Target) || o.Metadata.Field != c.Field || o.Metadata.Language != c.Language || o.Metadata.Format != c.Format || o.MIME != c.MIME || o.Size != c.SizeBytes || o.SHA256 != c.SHA256 || o.Metadata.Origin.Kind != "scraper" || o.Metadata.Origin.InstanceID != c.InstanceID || o.Metadata.Origin.CandidateID != c.CandidateID {
		return bad()
	}
	return nil
}
