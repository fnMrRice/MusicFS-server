package library

import (
	"context"
	"errors"
	"log/slog"
	"runtime"
	"slices"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/directoryidentity"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/jobs"
	"github.com/fnMrRice/MusicFS-server/src/plugins/sourceaccess"
)

func (s *Service) scan(ctx context.Context, j jobs.Job) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Hour)
	defer cancel()
	lease, err := s.HoldRead(ctx, j.Owner, j.ProviderInstanceID)
	if err != nil {
		return 0, err
	}
	defer lease.Close()
	ctx = lease.Context()
	r := lease.runtime
	if r.Worker == nil {
		return 0, domain.Fail("SOURCE_OFFLINE", "Provider is offline")
	}
	if err := validateRuntimePlan(r, j.Plan); err != nil {
		return 0, err
	}
	ctx, probe, closeProbe, err := s.beginProbeSession(ctx)
	if err != nil {
		return 0, err
	}
	defer closeProbe()
	defer s.Revoke(j.ProviderInstanceID, j.ID)
	slog.DebugContext(ctx, "Scan source prepared", "jobId", j.ID, "instanceId", r.ID, "roots", len(j.Plan.Roots), "probeEnabled", probe != nil)
	all := []Observation{}
	roots := j.Plan.Roots
	seenTracks := map[string]bool{}
	excluded := map[string][]string{}
	for _, root := range roots {
		var err error
		all, err = s.scanRoot(ctx, j, r, root.RootID, all, seenTracks, excluded, probe)
		if err != nil {
			return len(all), err
		}
	}
	slog.DebugContext(ctx, "Scan publication started", "jobId", j.ID, "observed", len(all))
	return len(all), s.repo.Publish(ctx, j, ScanPublication{PlanDigest: j.PlanDigest, CompletedRoots: roots, Observations: all, ExcludedPaths: excluded,
		StablePaths: runtime.GOOS == "windows" && r.Worker.Supports(directoryidentity.Capability)})
}

func (s *Service) scanRoot(ctx context.Context, j jobs.Job, r Runtime, rootID string, all []Observation, seenTracks map[string]bool, excluded map[string][]string, probe Probe) ([]Observation, error) {
	cursor := ""
	scope, err := j.Plan.Scope(rootID)
	if err != nil {
		return all, err
	}
	expectedPage := 0
	var coverage pluginproto.ScanCoverage
	seenCursors := map[string]bool{}
	for {
		started := time.Now()
		page, err := r.Worker.Scan(ctx, pluginproto.ScanRequest{RootID: rootID, ScanID: j.ID, Generation: j.Generation, Cursor: cursor, Limit: 200, Scope: scope, PlanDigest: j.PlanDigest})
		if err != nil {
			if ctx.Err() != nil {
				return all, ctx.Err()
			}
			return all, scanFailure(err)
		}
		if slog.Default().Enabled(ctx, slog.Level(-8)) {
			slog.Log(ctx, slog.Level(-8), "Scan page received", "jobId", j.ID, "instanceId", r.ID, "page", expectedPage, "items", len(page.Items), "issues", len(page.Issues), "complete", page.Complete, "durationMs", time.Since(started).Milliseconds())
		}
		if page.ScanID != j.ID || page.Generation != j.Generation || page.PlanDigest != j.PlanDigest || page.Page != expectedPage || len(page.Items) > pluginproto.MaxPageItems || len(page.Issues) > 0 || page.RootIdentity == "" {
			return all, domain.Fail("TEMPORARY_FAILURE", "Scan is incomplete")
		}
		if page.RootIdentity != scope.RootIdentity {
			return all, domain.Fail("RESOURCE_CHANGED", "Scan root identity changed")
		}
		if expectedPage == 0 {
			coverage, err = pluginproto.NewScanCoverage(scope, page.ExcludedPaths)
			if err != nil {
				return all, err
			}
			excluded[rootID] = append([]string{}, page.ExcludedPaths...)
		} else if !slices.Equal(excluded[rootID], page.ExcludedPaths) {
			return all, domain.Fail("RESOURCE_CHANGED", "Scan exclusions changed between pages")
		}
		expectedPage++
		if len(all)+len(page.Items) > 50000 {
			return all, domain.Fail("RATE_LIMITED", "Library entry limit exceeded")
		}
		for _, o := range page.Items {
			if o.ExternalID == "" || o.ObservedVersion == "" || !coverage.Contains(o.RootID, o.RelativePath) || seenTracks[o.ExternalID] {
				return all, domain.Fail("CONFLICT", "Scan contains an invalid or repeated track identity")
			}
			seenTracks[o.ExternalID] = true
			if _, err := providerEvidence(o); err != nil {
				return all, err
			}
		}
		// 每页只持久化一次发现数量，耗时的元数据读取开始前即可供客户端查询。
		if len(page.Items) > 0 {
			if err := s.jobs.UpdateProgress(ctx, j, len(all)+len(page.Items)); err != nil {
				return all, err
			}
		}
		for _, o := range page.Items {
			item := Observation{Observation: o, Artists: []string{}, ProviderObservation: &o}
			if probe != nil {
				item.ProbeStatus = "failed"
				scope := sourceaccess.Scope{UserID: j.Owner, InstanceID: r.ID, TaskID: j.ID, AssetID: o.ObservedVersion}
				h, e := r.Worker.Access.OpenIndexed(ctx, scope, o.RootID, o.RelativePath, o.ObservedVersion)
				if e != nil {
					return all, domain.Fail("RESOURCE_CHANGED", "Scanned source could not be reopened; retry scan")
				}
				if h.Entry.SizeBytes != o.SizeBytes {
					h.Close()
					return all, domain.Fail("RESOURCE_CHANGED", "Scanned source size changed before probing")
				}
				meta, e := probe(ctx, h, o.SizeBytes)
				valid := h.VerifyIndexedVersion()
				h.Close()
				var disabled *domain.Error
				if errors.Is(e, context.Canceled) || errors.Is(e, context.DeadlineExceeded) || (errors.As(e, &disabled) && disabled.Code == "PLUGIN_DISABLED") {
					return all, e
				}
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
					item.Album, item.Genre = meta.Album, meta.Genre
					item.Year, item.SampleRateHz = meta.Year, meta.SampleRateHz
					item.MIME = meta.MIME
					item.Evidence = meta.Evidence
				}
				if slog.Default().Enabled(ctx, slog.Level(-8)) {
					slog.Log(ctx, slog.Level(-8), "Scan source probe finished", "jobId", j.ID, "instanceId", r.ID, "status", item.ProbeStatus, "observed", len(all)+1)
				}
			}
			all = append(all, item)
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
