package enrichmentworker

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/enrichment"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/pluginproto"
)

func failureCode(err error) string {
	var known *domain.Error
	if errors.As(err, &known) {
		return known.Code
	}
	if errors.Is(err, context.Canceled) {
		return "CANCELED"
	}
	if strings.HasPrefix(err.Error(), "RATE_LIMITED:") {
		return "RATE_LIMITED"
	}
	return pluginproto.ErrorCode(err)
}

func (s *Service) execute(parent context.Context, w enrichment.Work) {
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	key := w.JobID + "/" + w.Target.SourceTrackID
	s.mu.Lock()
	s.running[key] = runningTask{jobID: w.JobID, cancel: cancel}
	s.mu.Unlock()
	defer func() { cancel(); s.mu.Lock(); delete(s.running, key); s.mu.Unlock() }()
	out := enrichment.Outcome{Candidates: []enrichment.PreviewCandidate{}, Issues: []enrichment.JobIssue{}}
	err := s.collect(ctx, w, &out)
	if parent.Err() != nil {
		return
	}
	if err != nil {
		code := failureCode(err)
		out.FailureCode = &code
	}
	finish, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	if err = s.repo.FinishEnrichment(finish, w, out); err != nil {
		latest, getErr := s.repo.GetEnrichmentJob(finish, w.Owner, w.JobID)
		if getErr != nil || latest.State != "cancelled" {
			slog.Error("Metadata task requires recovery", "jobId", w.JobID, "errorCode", failureCode(err))
		}
	}
}

func (s *Service) collect(ctx context.Context, w enrichment.Work, out *enrichment.Outcome) error {
	if err := s.repo.CurrentEnrichment(ctx, w); err != nil {
		return err
	}
	task := domain.NewID()
	input, err := s.library.OpenScrape(ctx, w.Owner, task, w.Target.SourceTrackID, w.Request.Fields)
	if err != nil {
		return err
	}
	defer input.Close()
	if input.Target.AssetID != w.Target.AssetID || input.Target.AssetVersion != w.Target.AssetVersion {
		return domain.Fail("RESOURCE_CHANGED", "Metadata source changed while queued")
	}
	input.Target = w.Target
	if err = input.DecodeNCM(ctx, s.config.NCMDecoder); err != nil {
		return err
	}
	for _, issue := range input.Issues {
		addIssue(out, "", issue.Code)
	}
	completed := false
	var lastFailure error
	for _, id := range w.Request.InstanceIDs {
		if err = ctx.Err(); err != nil {
			return err
		}
		r, ok := s.runtimes[id]
		if !ok || r.Owner != w.Owner || r.Host == nil {
			addIssue(out, id, "SOURCE_OFFLINE")
			lastFailure = domain.Fail("SOURCE_OFFLINE", "Selected scraper instance is offline")
			continue
		}
		if err = s.scrape(ctx, w, task, r, input, out); err != nil {
			lastFailure = err
			code := failureCode(err)
			addIssue(out, id, code)
			if code == "RESOURCE_CHANGED" || code == "CANCELED" {
				return err
			}
			continue
		}
		completed = true
	}
	if !completed {
		if lastFailure != nil {
			return lastFailure
		}
		return domain.Fail("UNSUPPORTED", "No scraper completed the metadata search")
	}
	return input.Validate()
}

func addIssue(out *enrichment.Outcome, instance, code string) {
	issue := enrichment.JobIssue{InstanceID: instance, Code: code}
	for _, prior := range out.Issues {
		if prior == issue {
			return
		}
	}
	if len(out.Issues) < 128 {
		out.Issues = append(out.Issues, issue)
	}
}

func (s *Service) scrape(ctx context.Context, w enrichment.Work, task string, r Runtime, input *library.ScrapeInput, out *enrichment.Outcome) error {
	scope := pluginproto.Context{UserID: w.Owner, InstanceID: r.ID, TaskID: task, TimeoutMs: 30000}
	session, err := r.Host.Open(ctx, scope, w.Target, input.Inputs)
	for err != nil && failureCode(err) == "RATE_LIMITED" {
		timer := time.NewTimer(50 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		session, err = r.Host.Open(ctx, scope, w.Target, input.Inputs)
	}
	if err != nil {
		return err
	}
	defer session.Close()
	result, err := session.Search(ctx, w.Request.Fields, w.Request.Languages, nil)
	if err != nil {
		return err
	}
	for _, issue := range result.Issues {
		addIssue(out, r.ID, issue.Code)
	}
	for _, candidate := range result.Candidates {
		if len(out.Candidates) >= enrichment.MaxCandidates {
			addIssue(out, r.ID, "RATE_LIMITED")
			break
		}
		if candidate.Field == "lyrics" && !languageSelected(candidate.Language, w.Request.Languages) {
			addIssue(out, r.ID, "UNSUPPORTED")
			continue
		}
		preview, err := s.preview(ctx, w, r.ID, input, session, candidate)
		if err != nil {
			code := failureCode(err)
			addIssue(out, r.ID, code)
			if code == "RESOURCE_CHANGED" || code == "CANCELED" || code == "CONFLICT" {
				return err
			}
			if code == "RATE_LIMITED" {
				break
			}
			continue
		}
		out.Candidates = append(out.Candidates, preview)
	}
	return nil
}
