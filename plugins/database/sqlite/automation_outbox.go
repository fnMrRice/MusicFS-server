package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/enrichment"
	"github.com/fnMrRice/MusicFS-server/src/jobs"
)

func enqueueScanAutomation(ctx context.Context, tx *sql.Tx, j jobs.Job, root, source, asset, version string) error {
	reasons := []string{"automatic"}
	options := j.Plan.Options
	if selected, ok := j.Plan.RootOptions[root]; ok {
		options = selected
	}
	if options.ImportLyrics {
		reasons = append(reasons, "importLyrics")
	}
	for _, reason := range reasons {
		_, err := tx.ExecContext(ctx, `INSERT INTO metadata_automation(id,owner,scan_job_id,scan_generation,plan_digest,root_id,source_id,asset_id,asset_version,reason,state,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,'queued',?) ON CONFLICT(scan_job_id,scan_generation,source_id,reason) DO NOTHING`, domain.NewID(), j.Owner, j.ID, j.Generation, j.PlanDigest, root, source, asset, version, reason, time.Now().UTC().Format(time.RFC3339Nano))
		if err != nil {
			return err
		}
	}
	return nil
}

const automationColumns = `a.id,a.owner,a.scan_job_id,a.source_id,a.asset_id,a.asset_version,a.reason,a.state,a.job_id,a.failure_code,a.policy`

func readAutomation(row rowScanner) (enrichment.AutomationWork, error) {
	w := enrichment.AutomationWork{AutomationResult: enrichment.AutomationResult{Actions: []enrichment.AutomationAction{}}}
	var policy []byte
	err := row.Scan(&w.TriggerID, &w.Owner, &w.ScanJobID, &w.SourceTrackID, &w.AssetID, &w.AssetVersion, &w.Reason, &w.State, &w.JobID, &w.FailureCode, &policy)
	if err == nil && len(policy) > 0 {
		err = json.Unmarshal(policy, &w.Policy)
	}
	return w, err
}

func (s *Store) NextAutomation(ctx context.Context) (enrichment.AutomationWork, error) {
	// A published library transaction is not a successful scan until Finish.
	// A recovered generation cannot consume an earlier publication's trigger.
	_, err := s.DB.ExecContext(ctx, `UPDATE metadata_automation SET state='failed',failure_code='RESOURCE_CHANGED' WHERE state IN ('queued','preparing') AND EXISTS(SELECT 1 FROM jobs j WHERE j.id=scan_job_id AND (j.generation!=scan_generation OR j.plan_digest!=metadata_automation.plan_digest OR j.state IN ('failed','cancelled','paused')))`)
	if err != nil {
		return enrichment.AutomationWork{}, err
	}
	return readAutomation(s.DB.QueryRowContext(ctx, `SELECT `+automationColumns+` FROM metadata_automation a JOIN jobs j ON j.id=a.scan_job_id AND j.owner=a.owner WHERE a.state IN ('queued','preparing','running') AND j.state='succeeded' AND j.generation=a.scan_generation AND j.plan_digest=a.plan_digest ORDER BY CASE a.state WHEN 'running' THEN 0 WHEN 'preparing' THEN 1 ELSE 2 END,a.created_at,a.id LIMIT 1`))
}

func (s *Store) PrepareAutomation(ctx context.Context, id string, p enrichment.AutomaticPolicy) error {
	p, err := p.Normalize()
	if err != nil {
		return err
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx, "UPDATE metadata_automation SET policy=?,state='preparing' WHERE id=? AND state='queued' AND policy IS NULL", raw, id)
	return err
}

func (s *Store) AttachAutomationJob(ctx context.Context, id, jobID string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	w, err := readAutomation(tx.QueryRowContext(ctx, "SELECT "+automationColumns+" FROM metadata_automation a WHERE id=?", id))
	if err != nil {
		return err
	}
	j, err := readEnrichmentJob(ctx, tx, w.Owner, jobID)
	if err != nil {
		return err
	}
	if len(j.Targets) != 1 || j.Targets[0].Target.SourceTrackID != w.SourceTrackID || j.Targets[0].Target.AssetID != w.AssetID || j.Targets[0].Target.AssetVersion != w.AssetVersion {
		return domain.Fail("RESOURCE_CHANGED", "Automatic metadata source changed")
	}
	if w.State == "running" && w.JobID != nil && *w.JobID == jobID {
		return nil
	}
	if w.State != "preparing" {
		return domain.Fail("CONFLICT", "Automatic metadata trigger is not pending")
	}
	if _, err = tx.ExecContext(ctx, "UPDATE metadata_automation SET job_id=?,state='running' WHERE id=?", jobID, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) FinishAutomation(ctx context.Context, id, state string, code *string) error {
	if state != "completed" && state != "skipped" && state != "failed" {
		return domain.Fail("INVALID_ARGUMENT", "Invalid automatic metadata result")
	}
	_, err := s.DB.ExecContext(ctx, "UPDATE metadata_automation SET state=?,failure_code=? WHERE id=? AND state IN ('queued','preparing','running')", state, code, id)
	return err
}

func (s *Store) AutomationAction(ctx context.Context, id string, a enrichment.AutomationAction) (enrichment.AutomationAction, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return a, err
	}
	defer tx.Rollback()
	var raw []byte
	err = tx.QueryRowContext(ctx, "SELECT document,expected_revision FROM metadata_automation_actions WHERE trigger_id=? AND field=? AND language=?", id, a.Field, a.Language).Scan(&raw, &a.ExpectedRevision)
	if err == nil {
		err = json.Unmarshal(raw, &a)
		return a, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return a, err
	}
	raw, err = json.Marshal(a)
	if err != nil {
		return a, err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO metadata_automation_actions(trigger_id,field,language,expected_revision,document) VALUES(?,?,?,?,?)", id, a.Field, a.Language, a.ExpectedRevision, raw)
	if err != nil {
		return a, err
	}
	return a, tx.Commit()
}

func (s *Store) FinishAutomationAction(ctx context.Context, id string, a enrichment.AutomationAction) error {
	raw, err := json.Marshal(a)
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx, "UPDATE metadata_automation_actions SET document=? WHERE trigger_id=? AND field=? AND language=?", raw, id, a.Field, a.Language)
	return err
}

var _ enrichment.AutomationRepository = (*Store)(nil)
