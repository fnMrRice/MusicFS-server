package library

import (
	"context"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/jobs"
	"github.com/fnMrRice/MusicFS-server/src/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/sourceaccess"
)

func (s *Service) scan(ctx context.Context, j jobs.Job) (int, error) {
	r, ok := s.runtimes[j.ProviderInstanceID]
	if !ok || r.Owner != j.Owner || r.Worker == nil {
		return 0, domain.Fail("SOURCE_OFFLINE", "Provider is offline")
	}
	if err := validateRuntimePlan(r, j.Plan); err != nil {
		return 0, err
	}
	ctx, cancel := context.WithTimeout(ctx, time.Hour)
	defer cancel()
	lease, err := s.HoldRead(ctx, j.Owner, j.ProviderInstanceID)
	if err != nil {
		return 0, err
	}
	defer lease.Close()
	defer s.Revoke(j.ProviderInstanceID, j.ID)
	all := []Observation{}
	roots := j.Plan.Roots
	seenTracks := map[string]bool{}
	for _, root := range roots {
		var err error
		all, err = s.scanRoot(ctx, j, r, root.RootID, all, seenTracks)
		if err != nil {
			return len(all), err
		}
	}
	return len(all), s.repo.Publish(ctx, j, ScanPublication{PlanDigest: j.PlanDigest, CompletedRoots: roots, Observations: all})
}

func (s *Service) scanRoot(ctx context.Context, j jobs.Job, r Runtime, rootID string, all []Observation, seenTracks map[string]bool) ([]Observation, error) {
	cursor := ""
	scope, err := j.Plan.Scope(rootID)
	if err != nil {
		return all, err
	}
	expectedPage := 0
	seenCursors := map[string]bool{}
	for {
		page, err := r.Worker.Scan(ctx, pluginproto.ScanRequest{RootID: rootID, ScanID: j.ID, Generation: j.Generation, Cursor: cursor, Limit: 200, Scope: scope, PlanDigest: j.PlanDigest})
		if err != nil {
			if ctx.Err() != nil {
				return all, ctx.Err()
			}
			return all, domain.Fail(pluginproto.ErrorCode(err), "Provider scan failed; previous library retained")
		}
		if page.ScanID != j.ID || page.Generation != j.Generation || page.PlanDigest != j.PlanDigest || page.Page != expectedPage || len(page.Items) > pluginproto.MaxPageItems || len(page.Issues) > 0 || page.RootIdentity == "" {
			return all, domain.Fail("TEMPORARY_FAILURE", "Scan is incomplete")
		}
		if page.RootIdentity != scope.RootIdentity {
			return all, domain.Fail("RESOURCE_CHANGED", "Scan root identity changed")
		}
		expectedPage++
		for _, o := range page.Items {
			if o.ExternalID == "" || o.ObservedVersion == "" || !scope.Contains(o.RootID, o.RelativePath) || seenTracks[o.ExternalID] {
				return all, domain.Fail("CONFLICT", "Scan contains an invalid or repeated track identity")
			}
			seenTracks[o.ExternalID] = true
			if _, err := providerEvidence(o); err != nil {
				return all, err
			}
			item := Observation{Observation: o, Artists: []string{}, ProviderObservation: &o}
			if s.probe != nil {
				item.ProbeStatus = "failed"
				scope := sourceaccess.Scope{UserID: j.Owner, InstanceID: r.ID, TaskID: j.ID, AssetID: o.ObservedVersion}
				ref, e := r.Worker.Access.Register(scope, o.RootID, o.RelativePath, o.ObservedVersion, time.Minute)
				if e != nil {
					return all, domain.Fail("RESOURCE_CHANGED", "Scanned source could not be reopened; retry scan")
				}
				h, e := r.Worker.Access.Open(ctx, scope, ref.ResourceRef)
				if e != nil {
					return all, domain.Fail("RESOURCE_CHANGED", "Scanned source could not be reopened; retry scan")
				}
				meta, e := s.probe(ctx, h, o.SizeBytes)
				valid := h.Validate()
				h.Close()
				if valid != nil {
					return all, domain.Fail("RESOURCE_CHANGED", "Scanned source changed while reading")
				}
				if e == nil {
					item.ProbeStatus = "succeeded"
					item.Codec = meta.Codec
					item.Container = meta.Container
					item.DurationMs = meta.DurationMs
					item.BitrateBps = meta.BitrateBps
					item.Title = meta.Title
					item.Artists = meta.Artists
					item.MIME = meta.MIME
					item.Evidence = meta.Evidence
				}
			}
			all = append(all, item)
			if len(all) > 50000 {
				return all, domain.Fail("RATE_LIMITED", "Library entry limit exceeded")
			}
		}
		if page.NextCursor == "" {
			if !page.Complete {
				return all, domain.Fail("TEMPORARY_FAILURE", "Scan did not complete")
			}
			break
		}
		if page.Complete || page.NextCursor == cursor || seenCursors[page.NextCursor] {
			return all, domain.Fail("CONFLICT", "Scan cursor did not advance")
		}
		seenCursors[page.NextCursor] = true
		cursor = page.NextCursor
	}
	return all, nil
}
