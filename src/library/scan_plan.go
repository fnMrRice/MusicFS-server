package library

import (
	"context"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

type ScanInput struct {
	Mode            string                      `json:"mode"`
	SchemaRevision  string                      `json:"schemaRevision"`
	RootIDs         []string                    `json:"rootIds"`
	Options         pluginproto.ImportOverrides `json:"options"`
	RefreshMetadata bool                        `json:"refreshMetadata,omitempty"`
}

// ScanPublication certifies that every requested root scope completed. Partial
// pages must never be published or used to infer missing library entries.
type ScanPublication struct {
	PlanDigest     string
	CompletedRoots []pluginproto.ScanRoot
	Observations   []Observation
	// StablePaths 表示来源支持同一目录路径替换文件后保留曲目标识。
	StablePaths bool
	// ExcludedPaths comes from host snapshots, never from a provider or request.
	ExcludedPaths map[string][]string
}

func (s *Service) BuildScanPlan(ctx context.Context, owner, provider string, input ScanInput) (pluginproto.ScanPlan, error) {
	if err := ctx.Err(); err != nil {
		return pluginproto.ScanPlan{}, err
	}
	r, ok := s.Runtime(provider)
	if !ok || r.Owner != owner {
		return pluginproto.ScanPlan{}, domain.Fail("NOT_FOUND", "Provider not found")
	}
	if err := s.checkProviderWork(ctx, r); err != nil {
		return pluginproto.ScanPlan{}, err
	}
	if r.Worker == nil {
		return pluginproto.ScanPlan{}, domain.Fail("SOURCE_OFFLINE", "Provider is offline")
	}
	select {
	case <-r.Worker.Done():
		return pluginproto.ScanPlan{}, domain.Fail("SOURCE_OFFLINE", "Provider is offline")
	default:
	}
	if r.Configuration == nil || input.SchemaRevision != r.Configuration.SchemaRevision {
		return pluginproto.ScanPlan{}, domain.Fail("CONFLICT", "Import configuration has changed; refresh the provider")
	}
	if input.RefreshMetadata {
		if input.Mode != "full" || len(input.RootIDs) != 1 {
			return pluginproto.ScanPlan{}, domain.Fail("INVALID_ARGUMENT", "Metadata refresh requires one complete directory scan")
		}
		// 仅覆盖本次任务，实例与目录保存的默认设置保持原值。
		enabled := true
		input.Options.ImportLyrics, input.Options.ImportArtwork = &enabled, &enabled
	}
	options, defaultErr := pluginproto.MergeImportOptions(r.ImportDefaults, input.Options)
	if defaultErr == nil {
		defaultErr = r.Configuration.ValidateOptions(options)
	}
	roots := map[string]string{}
	for _, root := range r.Worker.Access.Roots() {
		roots[root.ID] = root.ExpectedIdentity
	}
	plan := pluginproto.ScanPlan{PlanVersion: pluginproto.ScanPlanVersion, Mode: input.Mode, PluginID: r.Worker.Manifest.PluginID, PluginVersion: r.Worker.Manifest.PluginVersion, SchemaRevision: input.SchemaRevision, ConfigRevision: r.ConfigRevision, Options: options}
	plan.RootOptions = map[string]pluginproto.ImportOptions{}
	plan.RefreshMetadata = input.RefreshMetadata
	for _, id := range input.RootIDs {
		identity, ok := roots[id]
		if !ok {
			return pluginproto.ScanPlan{}, domain.Fail("ACCESS_DENIED", "Root is outside the provider authorization")
		}
		plan.Roots = append(plan.Roots, pluginproto.ScanRoot{RootID: id, RootIdentity: identity})
		if defaults, exists := r.RootImportDefaults[id]; exists {
			rootOptions, err := pluginproto.MergeImportOptions(defaults, input.Options)
			if err != nil {
				return pluginproto.ScanPlan{}, err
			}
			if err := r.Configuration.ValidateOptions(rootOptions); err != nil {
				return pluginproto.ScanPlan{}, err
			}
			plan.RootOptions[id] = rootOptions
		} else if defaultErr != nil {
			return pluginproto.ScanPlan{}, defaultErr
		}
	}
	// An unused instance default cannot invalidate explicitly configured roots.
	if defaultErr != nil && len(plan.Roots) > 0 {
		plan.Options = r.ImportDefaults
	}
	return plan.Canonical()
}

// Recovered tasks retain their exact plan. A new runtime configuration cannot
// silently reinterpret a queued request's roots, defaults, or plugin version.
func validateRuntimePlan(r Runtime, plan pluginproto.ScanPlan) error {
	if r.Configuration == nil || r.ConfigRevision != plan.ConfigRevision || r.Configuration.SchemaRevision != plan.SchemaRevision || r.Worker.Manifest.PluginID != plan.PluginID || r.Worker.Manifest.PluginVersion != plan.PluginVersion {
		return domain.Fail("RESOURCE_CHANGED", "Scan configuration changed; submit a new scan")
	}
	if err := r.Configuration.ValidateOptions(plan.Options); err != nil {
		return err
	}
	for _, options := range plan.RootOptions {
		if err := r.Configuration.ValidateOptions(options); err != nil {
			return err
		}
	}
	roots := map[string]string{}
	for _, root := range r.Worker.Access.Roots() {
		roots[root.ID] = root.ExpectedIdentity
	}
	for _, root := range plan.Roots {
		if roots[root.RootID] != root.RootIdentity {
			return domain.Fail("RESOURCE_CHANGED", "Scan root authorization changed; submit a new scan")
		}
	}
	return nil
}
