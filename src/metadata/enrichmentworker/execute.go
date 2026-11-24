package enrichmentworker

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/metadata/enrichment"
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
	started := time.Now()
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	slog.InfoContext(ctx, "Metadata target started", "jobId", w.JobID, "instances", len(w.Request.InstanceIDs))
	key := w.JobID + "/" + w.Target.SourceTrackID
	done := make(chan struct{})
	s.mu.Lock()
	s.running[key] = runningTask{jobID: w.JobID, cancel: cancel, done: done}
	s.mu.Unlock()
	defer func() { cancel(); s.mu.Lock(); delete(s.running, key); close(done); s.mu.Unlock() }()
	out := enrichment.Outcome{Candidates: []enrichment.PreviewCandidate{}, Issues: []enrichment.JobIssue{}}
	trace := newTrace(w)
	err := s.collect(ctx, w, &out, trace)
	if parent.Err() != nil {
		slog.DebugContext(parent, "Metadata target interrupted for shutdown", "jobId", w.JobID)
		return
	}
	if err != nil {
		code := failureCode(err)
		out.FailureCode = &code
	}
	finish, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	if err = s.saveDocument(finish, w, trace, out); err != nil {
		code := failureCode(err)
		out.FailureCode = &code
		addIssue(&out, "", code)
	}
	if err = s.repo.FinishEnrichment(finish, w, out); err != nil {
		latest, getErr := s.repo.GetEnrichmentJob(finish, w.Owner, w.JobID)
		if getErr != nil || latest.State != "cancelled" {
			slog.Error("Metadata task requires recovery", "jobId", w.JobID, "errorCode", failureCode(err))
		}
		return
	}
	level, message := slog.LevelInfo, "Metadata target completed"
	attrs := []any{"jobId", w.JobID, "candidates", len(out.Candidates), "issues", len(out.Issues), "durationMs", time.Since(started).Milliseconds()}
	if out.FailureCode != nil {
		level, message = slog.LevelWarn, "Metadata target failed"
		if *out.FailureCode == "CANCELED" {
			level, message = slog.LevelInfo, "Metadata target cancelled"
		}
		attrs = append(attrs, "errorCode", *out.FailureCode)
	}
	slog.Log(finish, level, message, attrs...)
}

func (s *Service) collect(ctx context.Context, w enrichment.Work, out *enrichment.Outcome, trace *documentTrace) error {
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
	if err = input.DecodeAudio(ctx, s.decoder()); err != nil {
		return err
	}
	trace.source(input)
	slog.DebugContext(ctx, "Metadata input prepared", "jobId", w.JobID, "inputs", len(input.Inputs), "issues", len(input.Issues))
	for _, issue := range input.Issues {
		addIssue(out, "", issue.Code)
	}
	if w.DirectoryRefresh && s.LyricsCatalog != nil {
		err = s.collectRefresh(ctx, w, task, input, out, trace)
	} else {
		err = s.collectInstances(ctx, w, task, input, w.Request.Fields, out, trace)
	}
	if err != nil {
		return err
	}
	return input.Validate()
}

func (s *Service) collectInstances(ctx context.Context, w enrichment.Work, task string, input *library.ScrapeInput, fields []string, out *enrichment.Outcome, trace *documentTrace) error {
	completed := false
	var lastFailure error
	for _, id := range w.Request.InstanceIDs {
		if err := ctx.Err(); err != nil {
			return err
		}
		r, ok := s.Runtime(id)
		attempt := trace.attempt(id, r.Descriptor.PluginID)
		if !ok || r.Owner != w.Owner || r.Host == nil {
			addIssue(out, id, "SOURCE_OFFLINE")
			lastFailure = domain.Fail("SOURCE_OFFLINE", "Selected scraper instance is offline")
			finishAttempt(attempt, lastFailure)
			continue
		}
		if !r.Descriptor.Supports(fields, w.Request.Query) {
			lastFailure = domain.Fail("UNSUPPORTED", "Scraper capabilities changed while queued")
			addIssue(out, id, "UNSUPPORTED")
			finishAttempt(attempt, lastFailure)
			continue
		}
		err := s.scrape(ctx, w, task, r, input, fields, out, trace, attempt)
		finishAttempt(attempt, err)
		if err != nil {
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
	return nil
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

func (s *Service) scrape(ctx context.Context, w enrichment.Work, task string, r Runtime, input *library.ScrapeInput, fields []string, out *enrichment.Outcome, trace *documentTrace, attempt *enrichment.ScraperAttempt) error {
	started := time.Now()
	slog.DebugContext(ctx, "Metadata scraper started", "jobId", w.JobID, "instanceId", r.ID)
	lease, err := r.Gate.AcquireEpoch(ctx, r.Epoch)
	if err != nil {
		return err
	}
	defer lease.Close()
	if lease != nil {
		ctx = lease.Context()
	}
	scope := pluginproto.Context{UserID: w.Owner, InstanceID: r.ID, TaskID: task, TimeoutMs: 30000}
	session, err := r.Host.Open(ctx, scope, w.Target, input.Inputs)
	for err != nil && failureCode(err) == "RATE_LIMITED" {
		if slog.Default().Enabled(ctx, slog.Level(-8)) {
			slog.Log(ctx, slog.Level(-8), "Metadata scraper waiting for capacity", "jobId", w.JobID, "instanceId", r.ID)
		}
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
	attempt.PluginVersion = session.PluginVersion()
	result, err := session.Search(ctx, fields, w.Request.Languages, w.Request.Query)
	if err != nil {
		return err
	}
	if err := enrichment.ValidateSearchResult(result, w.Target, fields); err != nil {
		return err
	}
	slog.DebugContext(ctx, "Metadata search completed", "jobId", w.JobID, "instanceId", r.ID, "candidates", len(result.Candidates), "issues", len(result.Issues), "durationMs", time.Since(started).Milliseconds())
	attempt.Search = trace.search(result)
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
		fetch := enrichment.DocumentFetch{CandidateID: candidate.CandidateID, State: "completed", Preview: &preview}
		if err != nil {
			code := failureCode(err)
			fetch.State = "failed"
			fetch.Code = &code
			fetch.Preview = nil
		}
		if err == nil && preview.FailureCode != nil {
			fetch.State = "failed"
			fetch.Code = preview.FailureCode
		}
		attempt.Fetches = append(attempt.Fetches, fetch)
		if slog.Default().Enabled(ctx, slog.Level(-8)) {
			slog.Log(ctx, slog.Level(-8), "Metadata candidate fetch finished", "jobId", w.JobID, "instanceId", r.ID, "state", fetch.State, "candidateIndex", len(attempt.Fetches))
		}
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
