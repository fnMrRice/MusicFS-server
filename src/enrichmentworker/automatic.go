package enrichmentworker

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/enrichment"
)

func (s *Service) ScanResults(ctx context.Context, owner, scan, cursor string, limit int) (enrichment.AutomationPage, error) {
	repo, ok := s.repo.(enrichment.AutomationRepository)
	if !ok {
		return enrichment.AutomationPage{}, domain.Fail("UNSUPPORTED", "Automatic metadata storage is unavailable")
	}
	return repo.ScanAutomationResults(ctx, owner, scan, cursor, limit)
}

func (s *Service) automationWorker(ctx context.Context, repo enrichment.AutomationRepository) {
	defer s.wg.Done()
	for ctx.Err() == nil {
		w, err := repo.NextAutomation(ctx)
		waiting := false
		if err == nil {
			waiting, err = s.automate(ctx, repo, w)
		}
		if err == nil && !waiting {
			continue
		}
		delay := 100 * time.Millisecond
		if err != nil && !errors.Is(err, sql.ErrNoRows) && ctx.Err() == nil {
			slog.Error("Automatic metadata requires retry", "errorCode", failureCode(err))
			delay = time.Second
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (s *Service) resolvedPolicy(w enrichment.AutomationWork) (enrichment.AutomaticPolicy, error) {
	p := s.config.Automatic
	if w.Reason == "importLyrics" {
		p.Enabled = true
		p.Fields = []string{"lyrics"}
		p.Languages = []string{}
	}
	if p.Enabled && len(p.InstancePriority) == 0 {
		for _, r := range s.ordered {
			if r.Owner == w.Owner && (r.Host != nil || r.Enabled) {
				p.InstancePriority = append(p.InstancePriority, r.ID)
			}
		}
	}
	return p.Normalize()
}

func (s *Service) automate(ctx context.Context, repo enrichment.AutomationRepository, w enrichment.AutomationWork) (bool, error) {
	fail := func(err error) (bool, error) {
		code := failureCode(err)
		return false, repo.FinishAutomation(ctx, w.TriggerID, "failed", &code)
	}
	if w.Policy == nil {
		p, err := s.resolvedPolicy(w)
		if err != nil {
			return fail(err)
		}
		if err = repo.PrepareAutomation(ctx, w.TriggerID, p); err != nil {
			return false, err
		}
		w.Policy = &p
	}
	p := *w.Policy
	if !p.Enabled {
		return false, repo.FinishAutomation(ctx, w.TriggerID, "skipped", nil)
	}
	if len(p.InstancePriority) == 0 {
		return fail(domain.Fail("UNSUPPORTED", "No available scraper instances"))
	}
	if w.JobID == nil {
		// Do not queue the current asset under an older scan's trigger.
		selection, err := s.content.ContentSelection(ctx, w.Owner, w.SourceTrackID)
		if err != nil {
			return fail(err)
		}
		if selection.Target.AssetID != w.AssetID || selection.Target.AssetVersion != w.AssetVersion {
			return fail(domain.Fail("RESOURCE_CHANGED", "Automatic metadata source changed"))
		}
		r := enrichment.JobRequest{SourceTrackIDs: []string{w.SourceTrackID}, Fields: p.Fields, Languages: p.Languages, InstanceIDs: p.InstancePriority}
		j, _, err := s.Submit(ctx, w.Owner, r, "automatic-"+w.TriggerID)
		if err != nil {
			if failureCode(err) == "RATE_LIMITED" {
				return true, nil
			}
			return fail(err)
		}
		if err = repo.AttachAutomationJob(ctx, w.TriggerID, j.JobID); err != nil {
			if failureCode(err) == "RESOURCE_CHANGED" {
				_, _ = s.Cancel(ctx, w.Owner, j.JobID)
				return fail(err)
			}
			return false, err
		}
		return true, nil
	}
	j, err := s.Get(ctx, w.Owner, *w.JobID)
	if err != nil {
		return false, err
	}
	if j.State == "queued" || j.State == "running" {
		return true, nil
	}
	if j.State != "completed" || len(j.Targets) != 1 || j.Targets[0].State != "completed" {
		code := "CONFLICT"
		if len(j.Targets) == 1 && j.Targets[0].FailureCode != nil {
			code = *j.Targets[0].FailureCode
		}
		return false, repo.FinishAutomation(ctx, w.TriggerID, "failed", &code)
	}
	if err = s.applyAutomaticTarget(ctx, repo, w, j.Targets[0]); err != nil {
		return false, err
	}
	return false, repo.FinishAutomation(ctx, w.TriggerID, "completed", nil)
}
