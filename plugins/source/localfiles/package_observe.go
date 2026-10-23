package localfiles

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"strings"

	"github.com/fnMrRice/MusicFS-server/sdk/directoryidentity"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
)

func (w *worker) observePackage(ctx context.Context, request pluginproto.PackageObserveRequest) (pluginproto.PackageObserveResult, error) {
	result := pluginproto.PackageObserveResult{Items: []pluginproto.Observation{}}
	if len(request.Entries) < 1 || len(request.Entries) > pluginproto.MaxPageItems {
		return result, fmt.Errorf("INVALID_ARGUMENT: invalid package observation count")
	}
	seen := map[string]bool{}
	for _, entry := range request.Entries {
		key := entry.RootID + "\x00" + entry.RelativePath
		if entry.RootID == "" || entry.ObservedVersion == "" || entry.SizeBytes < 0 || !fs.ValidPath(entry.RelativePath) || entry.RelativePath == "." || strings.ContainsAny(entry.RelativePath, "\\:") || seen[key] {
			return result, fmt.Errorf("ACCESS_DENIED: invalid package observation")
		}
		seen[key] = true
		item, err := w.observeEntry(ctx, entry)
		if err != nil {
			return pluginproto.PackageObserveResult{}, err
		}
		result.Items = append(result.Items, item)
	}
	return result, nil
}

// observeEntry 与常规扫描共享身份和解析状态，不另建作品专用来源身份。
func (w *worker) observeEntry(ctx context.Context, entry pluginproto.Entry) (pluginproto.Observation, error) {
	if err := ctx.Err(); err != nil {
		return pluginproto.Observation{}, err
	}
	externalID, err := w.identity(ctx, entry)
	if err != nil {
		return pluginproto.Observation{}, err
	}
	item := pluginproto.Observation{ExternalID: externalID, RootID: entry.RootID, RelativePath: entry.RelativePath,
		DisplayName: path.Base(entry.RelativePath), SourceRef: externalID, ObservedVersion: entry.ObservedVersion,
		SizeBytes: entry.SizeBytes, MIME: entry.MIME, DurationMs: entry.DurationMs}
	raw, err := json.Marshal(item)
	if err != nil {
		return pluginproto.Observation{}, err
	}
	var state pluginproto.StateValue
	if err = w.peer.Call(ctx, "host.state.get", pluginproto.StateRequest{Key: directoryidentity.TrackKey(externalID)}, &state); err != nil {
		return pluginproto.Observation{}, err
	}
	if err = w.peer.Call(ctx, "host.state.compareAndSet", pluginproto.StateRequest{Key: directoryidentity.TrackKey(externalID), Revision: state.Revision, Value: raw}, nil); err != nil {
		return pluginproto.Observation{}, err
	}
	return item, nil
}
