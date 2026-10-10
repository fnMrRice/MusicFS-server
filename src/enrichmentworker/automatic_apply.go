package enrichmentworker

import (
	"context"
	"crypto/sha256"
	"fmt"
	"slices"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/enrichment"
	"github.com/fnMrRice/MusicFS-server/src/mediastore"
)

func (s *Service) applyAutomaticTarget(ctx context.Context, repo enrichment.AutomationRepository, w enrichment.AutomationWork, target enrichment.JobTarget) error {
	groups := map[string][]enrichment.PreviewCandidate{}
	for _, c := range target.Candidates {
		if enrichment.EligibleAutomatic(c, *w.Policy) {
			key := c.Field + "/" + c.Language
			groups[key] = append(groups[key], c)
		}
	}
	keys := []string{}
	for key := range groups {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		candidates := groups[key]
		slices.SortFunc(candidates, func(a, b enrichment.PreviewCandidate) int {
			pa, pb := slices.Index(w.Policy.InstancePriority, a.InstanceID), slices.Index(w.Policy.InstancePriority, b.InstanceID)
			if pa != pb {
				return pa - pb
			}
			if a.CandidateID < b.CandidateID {
				return -1
			}
			if a.CandidateID > b.CandidateID {
				return 1
			}
			return 0
		})
		c := candidates[0]
		a := enrichment.AutomationAction{Field: c.Field, Language: c.Language, Status: "pending", Reason: "eligible", CandidateID: &c.CandidateID}
		for _, other := range candidates[1:] {
			if other.InstanceID == c.InstanceID && other.SHA256 != c.SHA256 {
				a.Status = "skipped"
				a.Reason = "ambiguous"
			}
		}
		selection, err := s.content.ContentSelection(ctx, w.Owner, w.SourceTrackID)
		if err != nil {
			return err
		}
		a.ExpectedRevision = selection.Revision
		for _, b := range selection.Bindings {
			if b.Field == a.Field && b.Language == a.Language {
				if b.ManualLocked {
					a.Status = "skipped"
					a.Reason = "manualLocked"
				} else if b.Current && b.State == "available" {
					a.Status = "skipped"
					a.Reason = "existingContent"
				}
			}
		}
		a, err = repo.AutomationAction(ctx, w.TriggerID, a)
		if err != nil {
			return err
		}
		if a.Status != "pending" {
			continue
		}
		result, err := s.applyAutomaticCandidate(ctx, repo, w, a, c)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			a.Status = "failed"
			a.Reason = failureCode(err)
			if a.Reason == "CONFLICT" {
				a.Status = "skipped"
			}
		} else {
			a.Status = "applied"
			a.Reason = "selected"
			for _, b := range result.Bindings {
				if b.Field == a.Field && b.Language == a.Language {
					id := b.ID
					a.BindingID = &id
				}
			}
		}
		if err = repo.FinishAutomationAction(ctx, w.TriggerID, a); err != nil {
			return err
		}
	}
	for _, field := range w.Policy.Fields {
		found := false
		for _, candidates := range groups {
			if candidates[0].Field == field {
				found = true
			}
		}
		if !found {
			a := enrichment.AutomationAction{Field: field, Language: mediastore.CanonicalLanguage(field, ""), Status: "skipped", Reason: "noEligibleCandidate"}
			if len(target.Issues) > 0 {
				a.Status = "failed"
				a.Reason = target.Issues[0].Code
			}
			if _, err := repo.AutomationAction(ctx, w.TriggerID, a); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Service) applyAutomaticCandidate(parent context.Context, repo enrichment.AutomationRepository, w enrichment.AutomationWork, a enrichment.AutomationAction, c enrichment.PreviewCandidate) (mediastore.Selection, error) {
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	if a.CandidateID == nil || *a.CandidateID != c.CandidateID {
		return mediastore.Selection{}, domain.Fail("CONFLICT", "Automatic candidate changed")
	}
	input, err := s.library.OpenScrape(ctx, w.Owner, domain.NewID(), w.SourceTrackID, []string{a.Field})
	sourceErr := err
	if input != nil {
		defer input.Close()
		if input.Target.AssetID != w.AssetID || input.Target.AssetVersion != w.AssetVersion {
			sourceErr = domain.Fail("RESOURCE_CHANGED", "Automatic metadata source changed")
		}
	}
	digest := sha256.Sum256([]byte(w.TriggerID + "/" + a.Field + "/" + a.Language))
	// Publish rechecks the library before the selection transaction (the SQLite
	// connection is singular). A source error becomes a failing guard so an
	// already committed operation can still replay without reselecting anything.
	apply := func(sourceFailure error) (mediastore.Selection, error) {
		return s.content.ApplyCandidate(ctx, w.Owner, *c.PreviewObjectID, func(fileGuard mediastore.Guard) (mediastore.Selection, error) {
			guard := func(publish func() error) error {
				if sourceFailure != nil {
					return sourceFailure
				}
				return fileGuard(publish)
			}
			return repo.ApplyAutomaticEnrichment(ctx, w.Owner, c.CandidateID, *c.PreviewObjectID, a.ExpectedRevision, fmt.Sprintf("automatic-%x", digest), guard)
		})
	}
	if sourceErr != nil {
		return apply(sourceErr)
	}
	var result mediastore.Selection
	called := false
	err = input.Publish(func() error { called = true; var e error; result, e = apply(nil); return e })
	if !called && err != nil {
		return apply(err)
	}
	return result, err
}
