package sqlite

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/metadata/enrichment"
)

func (s *Store) QueueEnrichment(ctx context.Context, owner string, request enrichment.JobRequest, key string) (enrichment.Job, bool, error) {
	return s.queueEnrichment(ctx, owner, request, key, "")
}

func (s *Store) QueueAutomaticEnrichment(ctx context.Context, owner, triggerID string, request enrichment.JobRequest) (enrichment.Job, bool, error) {
	return s.queueEnrichment(ctx, owner, request, "automatic-"+triggerID, triggerID)
}

func (s *Store) queueEnrichment(ctx context.Context, owner string, request enrichment.JobRequest, key, triggerID string) (enrichment.Job, bool, error) {
	var empty enrichment.Job
	if owner == "" || len(key) < 8 || len(key) > 128 {
		return empty, false, domain.Fail("INVALID_ARGUMENT", "Invalid metadata operation")
	}
	request, err := request.Normalize()
	if err != nil {
		return empty, false, err
	}
	if len(request.InstanceIDs) == 0 && triggerID == "" {
		return empty, false, domain.Fail("UNSUPPORTED", "No scraper instances selected")
	}
	raw, err := json.Marshal(request)
	if err != nil {
		return empty, false, err
	}
	sum := sha256.Sum256(raw)
	digest := hex.EncodeToString(sum[:])
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return empty, false, err
	}
	defer tx.Rollback()
	var automation *enrichment.AutomationWork
	if triggerID != "" {
		w, err := readAutomation(tx.QueryRowContext(ctx, "SELECT "+automationColumns+" FROM metadata_automation a WHERE owner=? AND id=?", owner, triggerID))
		if err != nil {
			return empty, false, err
		}
		if err = activeAutomation(ctx, tx, triggerID); err != nil {
			return empty, false, err
		}
		if len(request.InstanceIDs) == 0 && w.Reason != "refreshMetadata" {
			return empty, false, domain.Fail("UNSUPPORTED", "No scraper instances selected")
		}
		if len(request.SourceTrackIDs) != 1 || request.SourceTrackIDs[0] != w.SourceTrackID {
			return empty, false, domain.Fail("CONFLICT", "Automatic metadata target changed")
		}
		if w.JobID != nil {
			j, err := readEnrichmentJob(ctx, tx, owner, *w.JobID)
			if err == nil {
				stored, encodeErr := json.Marshal(j.Request)
				if encodeErr != nil {
					return empty, false, encodeErr
				}
				if string(stored) != string(raw) {
					return empty, false, domain.Fail("CONFLICT", "Automatic metadata request changed")
				}
			}
			return j, true, err
		}
		automation = &w
	}
	prior, replay, err := contentIdempotency(ctx, tx, owner, "metadata/jobs", key, digest)
	if err != nil {
		return empty, false, err
	}
	if replay {
		var id string
		if err = json.Unmarshal(prior, &id); err != nil {
			return empty, false, err
		}
		j, e := readEnrichmentJob(ctx, tx, owner, id)
		if e == nil && automation != nil {
			if len(j.Targets) != 1 || j.Targets[0].Target.SourceTrackID != automation.SourceTrackID || j.Targets[0].Target.AssetID != automation.AssetID || j.Targets[0].Target.AssetVersion != automation.AssetVersion {
				return empty, false, domain.Fail("RESOURCE_CHANGED", "Automatic metadata source changed")
			}
			if _, e = tx.ExecContext(ctx, "UPDATE metadata_automation SET job_id=?,state='running' WHERE id=? AND owner=?", id, triggerID, owner); e == nil {
				e = tx.Commit()
			}
		}
		return j, true, e
	}
	sources, err := enrichmentSources(ctx, tx, owner, request)
	if err != nil {
		return empty, false, err
	}
	var owned, all int
	if err = tx.QueryRowContext(ctx, "SELECT COALESCE(SUM(CASE WHEN owner=? THEN 1 ELSE 0 END),0),COUNT(*) FROM enrichment_targets WHERE state IN ('queued','running')", owner).Scan(&owned, &all); err != nil {
		return empty, false, err
	}
	if owned+len(sources) > 256 || all+len(sources) > 1024 {
		return empty, false, domain.Fail("RATE_LIMITED", "Metadata queue is full")
	}
	id := domain.NewID()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err = tx.ExecContext(ctx, "INSERT INTO enrichment_jobs(id,owner,state,request,created_at,updated_at) VALUES(?,?,'queued',?,?,?)", id, owner, raw, now, now); err != nil {
		return empty, false, err
	}
	for _, source := range sources {
		selection, e := readContentSelection(ctx, tx, owner, source)
		if e != nil {
			return empty, false, e
		}
		if e = contentCurrentTarget(ctx, tx, owner, selection.Target); e != nil {
			return empty, false, e
		}
		if automation != nil && (automation.AssetID != selection.Target.AssetID || automation.AssetVersion != selection.Target.AssetVersion) {
			return empty, false, domain.Fail("RESOURCE_CHANGED", "Automatic metadata source changed")
		}
		target := enrichment.Target{SourceTrackID: source, AssetID: selection.Target.AssetID, AssetVersion: selection.Target.AssetVersion, Revision: selection.Revision}
		raw, e := json.Marshal(target)
		if e != nil {
			return empty, false, e
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO enrichment_targets(job_id,source_id,owner,state,generation,target,issues) VALUES(?,?,?,'queued',1,?,?)", id, source, owner, raw, []byte("[]")); err != nil {
			return empty, false, err
		}
	}
	result, _ := json.Marshal(id)
	if err = saveContentIdempotency(ctx, tx, owner, "metadata/jobs", key, digest, result); err != nil {
		return empty, false, err
	}
	if triggerID != "" {
		if _, err = tx.ExecContext(ctx, "UPDATE metadata_automation SET job_id=?,state='running' WHERE id=? AND owner=?", id, triggerID, owner); err != nil {
			return empty, false, err
		}
	}
	j, err := readEnrichmentJob(ctx, tx, owner, id)
	if err != nil {
		return j, false, err
	}
	return j, false, tx.Commit()
}

func (s *Store) ClaimEnrichment(ctx context.Context) (enrichment.Work, error) {
	var w enrichment.Work
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return w, err
	}
	defer tx.Rollback()
	var target, request []byte
	err = tx.QueryRowContext(ctx, `SELECT j.owner,j.id,t.target,t.generation,j.request,
 EXISTS(SELECT 1 FROM metadata_automation a WHERE a.state='running' AND a.job_id=j.id AND a.owner=j.owner AND a.reason='refreshMetadata')
 FROM enrichment_jobs j JOIN enrichment_targets t ON t.job_id=j.id WHERE j.state IN ('queued','running') AND t.state='queued' ORDER BY j.created_at,j.id,t.source_id LIMIT 1`).Scan(&w.Owner, &w.JobID, &target, &w.Generation, &request, &w.DirectoryRefresh)
	if err != nil {
		return w, err
	}
	if err = json.Unmarshal(target, &w.Target); err != nil {
		return w, err
	}
	if err = json.Unmarshal(request, &w.Request); err != nil {
		return w, err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE enrichment_targets SET state='running' WHERE job_id=? AND source_id=? AND generation=? AND state='queued'", w.JobID, w.Target.SourceTrackID, w.Generation); err != nil {
		return w, err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE enrichment_jobs SET state='running',updated_at=? WHERE id=?", time.Now().UTC().Format(time.RFC3339Nano), w.JobID); err != nil {
		return w, err
	}
	return w, tx.Commit()
}

func (s *Store) ReserveEnrichmentPreview(ctx context.Context, w enrichment.Work, size int64) error {
	if size < 0 || size > 8<<20 {
		return domain.Fail("INVALID_ARGUMENT", "Invalid metadata preview reservation")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = activeEnrichment(ctx, tx, w); err != nil {
		return err
	}
	var totalBytes int64
	var totalCount, targetCount int
	err = tx.QueryRowContext(ctx, "SELECT COALESCE(SUM(preview_bytes),0),COALESCE(SUM(candidate_count),0),COALESCE(MAX(CASE WHEN source_id=? THEN candidate_count ELSE 0 END),0) FROM enrichment_targets WHERE owner=? AND job_id=?", w.Target.SourceTrackID, w.Owner, w.JobID).Scan(&totalBytes, &totalCount, &targetCount)
	if err != nil {
		return err
	}
	if size > enrichment.MaxJobPreviewBytes-totalBytes || totalCount >= enrichment.MaxJobCandidates || targetCount >= enrichment.MaxCandidates {
		return domain.Fail("RATE_LIMITED", "Metadata preview budget exceeded")
	}
	if _, err = tx.ExecContext(ctx, "UPDATE enrichment_targets SET preview_bytes=preview_bytes+?,candidate_count=candidate_count+1 WHERE job_id=? AND source_id=?", size, w.JobID, w.Target.SourceTrackID); err != nil {
		return err
	}
	return tx.Commit()
}

var _ enrichment.Repository = (*Store)(nil)
