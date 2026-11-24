package library

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/plugins/sourceaccess"
)

// PackageSourceObservations 只观察已完整核对的快照音频，沿用常规扫描身份和元数据探测。
func (s *Service) PackageSourceObservations(ctx context.Context, owner string, snapshot PackageSnapshot) ([]Observation, error) {
	lease, err := s.packageLease(ctx, owner, snapshot.ProviderID)
	if err != nil {
		return nil, err
	}
	defer lease.Close()
	ctx, cancel := context.WithTimeout(lease.Context(), sourceaccess.PackageReadTimeout)
	defer cancel()
	r := lease.runtime
	if err := verifyPackageSnapshot(ctx, r, snapshot); err != nil {
		return nil, err
	}
	ctx, probe, closeProbe, err := s.beginProbeSession(ctx)
	if err != nil {
		return nil, packageSourceError(err)
	}
	defer closeProbe()
	task := domain.NewID()
	defer r.Worker.Access.Revoke(task)
	entries := []pluginproto.Entry{}
	for _, entry := range snapshot.Entries {
		if entry.Kind == "audio" {
			entries = append(entries, entry.Entry)
		}
	}
	all := []Observation{}
	seen := map[string]bool{}
	for start := 0; start < len(entries); {
		batch, err := packageObservationBatch(entries[start:])
		if err != nil {
			return nil, err
		}
		start += len(batch)
		items, err := r.Worker.ObservePackage(ctx, batch)
		if err != nil {
			return nil, packageSourceError(err)
		}
		for _, item := range items {
			if seen[item.ExternalID] {
				return nil, domain.Fail("CONFLICT", "Package contains repeated track identities")
			}
			seen[item.ExternalID] = true
			if _, err := providerEvidence(item); err != nil {
				return nil, packageSourceError(err)
			}
			observation, err := probePackageEntry(ctx, r, owner, task, item, probe)
			if err != nil {
				return nil, packageSourceError(err)
			}
			all = append(all, observation)
		}
	}
	if err := verifyPackageSnapshot(ctx, r, snapshot); err != nil {
		return nil, err
	}
	return all, nil
}

// 预留响应名称、JSON 转义和信封空间，长中文或特殊字符路径也不会撑破 RPC 帧。
func packageObservationBatch(entries []pluginproto.Entry) ([]pluginproto.Entry, error) {
	bytes, count := 128, 0
	for _, entry := range entries[:min(len(entries), pluginproto.MaxPageItems)] {
		raw, err := json.Marshal(entry)
		if err != nil {
			return nil, domain.Fail("INVALID_ARGUMENT", "Package observation could not be encoded")
		}
		if bytes+len(raw)+1 > pluginproto.MaxMessageBytes/3 {
			break
		}
		bytes += len(raw) + 1
		count++
	}
	if count == 0 {
		return nil, domain.Fail("RATE_LIMITED", "Package observation exceeds message limit")
	}
	return entries[:count], nil
}

func verifyPackageSnapshot(ctx context.Context, r Runtime, expected PackageSnapshot) error {
	actual, err := packageSnapshot(ctx, r, expected.RootID, expected.RelativePath)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(actual, expected) {
		return domain.Fail("RESOURCE_CHANGED", "Package snapshot changed; preview the package again")
	}
	return nil
}

func probePackageEntry(ctx context.Context, r Runtime, owner, task string, item pluginproto.Observation, probe Probe) (Observation, error) {
	result := Observation{Observation: item, Artists: []string{}, ProviderObservation: &item}
	if probe == nil {
		return result, nil
	}
	result.ProbeStatus = "failed"
	scope := sourceaccess.Scope{UserID: owner, InstanceID: r.ID, TaskID: task, AssetID: item.ObservedVersion}
	h, err := r.Worker.Access.OpenPackageFile(ctx, scope, item.RootID, item.RelativePath, item.ObservedVersion, packageImportOptions(r, item.RootID))
	if err != nil {
		return result, err
	}
	defer h.Close()
	defer r.Worker.Access.Revoke(task)
	meta, probeErr := probe(ctx, h, item.SizeBytes)
	if err := h.Validate(); err != nil {
		return result, domain.Fail("RESOURCE_CHANGED", "Package audio changed during probing")
	}
	var disabled *domain.Error
	if errors.Is(probeErr, context.Canceled) || errors.Is(probeErr, context.DeadlineExceeded) || (errors.As(probeErr, &disabled) && disabled.Code == "PLUGIN_DISABLED") {
		return result, probeErr
	}
	if probeErr == nil {
		result.ProbeStatus = "succeeded"
		result.NetEaseID, result.NetEaseIDConflict = meta.NetEaseID, meta.NetEaseIDConflict
		result.Codec, result.Container = meta.Codec, meta.Container
		result.DurationMs, result.BitrateBps = meta.DurationMs, meta.BitrateBps
		result.Title, result.Artists = meta.Title, meta.Artists
		result.Album, result.Genre = meta.Album, meta.Genre
		result.Year, result.SampleRateHz = meta.Year, meta.SampleRateHz
		result.MIME, result.Evidence = meta.MIME, meta.Evidence
	}
	return result, nil
}
