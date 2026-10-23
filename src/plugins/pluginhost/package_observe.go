package pluginhost

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

// ObservePackage 只向声明作品包能力的实例传递当前授权根内的精确音频批次。
func (w *Worker) ObservePackage(ctx context.Context, entries []pluginproto.Entry) ([]pluginproto.Observation, error) {
	if w == nil || w.Access == nil || !w.Supports("library.packages") {
		return nil, domain.Fail("UNSUPPORTED", "Provider does not support package observations")
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
		if !providerReference(item.ExternalID, 256) || !providerReference(item.SourceRef, 256) || identities[item.ExternalID] || item.RootID != entry.RootID || item.RelativePath != entry.RelativePath || item.ObservedVersion != entry.ObservedVersion || item.SizeBytes != entry.SizeBytes || item.MIME != entry.MIME {
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

// 来源 ID 和引用都是插件的不透明标识，宿主只约束长度与文本有效性。
func providerReference(value string, limit int) bool {
	return len(value) <= limit && strings.TrimSpace(value) != "" && utf8.ValidString(value) && strings.IndexFunc(value, unicode.IsControl) < 0
}
