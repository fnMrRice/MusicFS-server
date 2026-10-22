package pluginhost

import (
	"context"
	"encoding/json"
	"reflect"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

// ObservePackage 只向已验证的 Local Files 实例传递当前授权根内的精确音频批次。
func (w *Worker) ObservePackage(ctx context.Context, entries []pluginproto.Entry) ([]pluginproto.Observation, error) {
	if w == nil || w.Access == nil || w.Manifest.PluginID != "musicfs.plugin.source.localfiles" {
		return nil, domain.Fail("UNSUPPORTED", "Package observations require Local Files")
	}
	if len(entries) < 1 || len(entries) > pluginproto.MaxPageItems {
		return nil, domain.Fail("INVALID_ARGUMENT", "Invalid package observation count")
	}
	request := pluginproto.PackageObserveRequest{Entries: entries}
	raw, err := json.Marshal(request)
	if err != nil || len(raw) > pluginproto.MaxMessageBytes/3 {
		return nil, domain.Fail("RATE_LIMITED", "Package observations exceed message limit")
	}
	if err := w.state.ready(); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, entry := range entries {
		key := entry.RootID + "\x00" + entry.RelativePath
		if seen[key] {
			return nil, domain.Fail("INVALID_ARGUMENT", "Repeated package source")
		}
		seen[key] = true
		actual, err := w.Access.Inspect(entry.RootID, entry.RelativePath)
		if err != nil || !reflect.DeepEqual(actual, entry) {
			return nil, domain.Fail("RESOURCE_CHANGED", "Package source changed before observation")
		}
	}
	var result pluginproto.PackageObserveResult
	if err := w.peer.Call(ctx, "provider.observePackage", request, &result); err != nil {
		return nil, err
	}
	if len(result.Items) != len(entries) {
		return nil, domain.Fail("PROTOCOL_MISMATCH", "Incomplete package observations")
	}
	identities := map[string]bool{}
	for i, item := range result.Items {
		entry := entries[i]
		if !localExternalID.MatchString(item.ExternalID) || identities[item.ExternalID] || item.RootID != entry.RootID || item.RelativePath != entry.RelativePath || item.ObservedVersion != entry.ObservedVersion || item.SizeBytes != entry.SizeBytes || item.MIME != entry.MIME || item.SourceRef != item.ExternalID {
			return nil, domain.Fail("PROTOCOL_MISMATCH", "Package observation does not match its source")
		}
		identities[item.ExternalID] = true
		actual, err := w.Access.Inspect(entry.RootID, entry.RelativePath)
		if err != nil || !reflect.DeepEqual(actual, entry) {
			return nil, domain.Fail("RESOURCE_CHANGED", "Package source changed during observation")
		}
	}
	return result.Items, nil
}
