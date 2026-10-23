package enrichmentworker

import (
	"context"
	"encoding/json"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/metadata/enrichment"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore"
)

type documentTrace struct {
	doc       enrichment.ScrapeDocument
	resources map[string]int
}

func newTrace(w enrichment.Work) *documentTrace {
	return &documentTrace{doc: enrichment.ScrapeDocument{Schema: "scrape-document/v1", JobID: w.JobID, Target: w.Target, Generation: w.Generation, Request: w.Request, Resources: []enrichment.DocumentResource{}, SourceIssues: []enrichment.DocumentIssue{}, Attempts: []enrichment.ScraperAttempt{}, StartedAt: time.Now().UTC()}, resources: map[string]int{}}
}
func (d *documentTrace) source(input *library.ScrapeInput) {
	for _, in := range input.Inputs {
		r := in.Resource
		number := len(d.doc.Resources) + 1
		d.resources[r.ResourceRef] = number
		d.doc.Resources = append(d.doc.Resources, enrichment.DocumentResource{Number: number, Version: r.Version, Kind: r.Kind, SizeBytes: r.SizeBytes, Match: r.Match})
	}
	for _, issue := range input.Issues {
		d.doc.SourceIssues = append(d.doc.SourceIssues, enrichment.DocumentIssue{ResourceNumber: d.resources[issue.ResourceRef], Code: issue.Code})
	}
}
func (d *documentTrace) attempt(id, pluginID string) *enrichment.ScraperAttempt {
	d.doc.Attempts = append(d.doc.Attempts, enrichment.ScraperAttempt{InstanceID: id, PluginID: pluginID, State: "started", Fetches: []enrichment.DocumentFetch{}})
	return &d.doc.Attempts[len(d.doc.Attempts)-1]
}
func (d *documentTrace) search(result enrichment.SearchResult) *enrichment.DocumentSearch {
	s := &enrichment.DocumentSearch{Candidates: []enrichment.DocumentCandidate{}, Issues: []enrichment.DocumentIssue{}}
	for _, c := range result.Candidates {
		s.Candidates = append(s.Candidates, enrichment.DocumentCandidate{CandidateID: c.CandidateID, Target: c.Target, Field: c.Field, Language: c.Language, Format: c.Format, MIME: c.MIME, SizeBytes: c.SizeBytes, SHA256: c.SHA256, Origin: c.Origin, ResourceNumber: d.resources[c.ResourceRef], ResourceVersion: c.ResourceVersion, Basis: c.Basis, Status: c.Status, FailureCode: c.FailureCode})
	}
	for _, issue := range result.Issues {
		s.Issues = append(s.Issues, enrichment.DocumentIssue{ResourceNumber: d.resources[issue.ResourceRef], Code: issue.Code})
	}
	return s
}
func finishAttempt(a *enrichment.ScraperAttempt, err error) {
	a.State = "completed"
	if err != nil {
		code := failureCode(err)
		a.State = "failed"
		a.FailureCode = &code
	}
	if a.Search != nil {
		for _, candidate := range a.Search.Candidates {
			found := false
			for _, fetch := range a.Fetches {
				if fetch.CandidateID == candidate.CandidateID {
					found = true
					break
				}
			}
			if !found {
				a.Fetches = append(a.Fetches, enrichment.DocumentFetch{CandidateID: candidate.CandidateID, State: "notRequested"})
			}
		}
	}
}
func (s *Service) saveDocument(ctx context.Context, w enrichment.Work, d *documentTrace, out enrichment.Outcome) error {
	derived := s.content.Derived()
	if derived == nil {
		return nil
	}
	job, err := s.repo.GetEnrichmentJob(ctx, w.Owner, w.JobID)
	if err != nil {
		return err
	}
	d.doc.Targets = make([]enrichment.Target, 0, len(job.Targets))
	for _, target := range job.Targets {
		d.doc.Targets = append(d.doc.Targets, target.Target)
	}
	d.doc.Outcome = enrichment.DocumentOutcome{Candidates: out.Candidates, Issues: out.Issues, FailureCode: out.FailureCode}
	d.doc.FinishedAt = time.Now().UTC()
	body, err := json.Marshal(d.doc)
	if err != nil {
		return err
	}
	_, err = derived.PublishDocument(ctx, w.Owner, mediastore.DerivedMetadata{Transform: d.doc.Schema, JobID: w.JobID, SourceTrackID: w.Target.SourceTrackID, AssetID: w.Target.AssetID, AssetVersion: w.Target.AssetVersion, Generation: w.Generation}, body)
	return err
}
