package localfiles

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/fnMrRice/MusicFS-server/src/pluginproto"
)

func (w *worker) resolve(ctx context.Context, r pluginproto.ResolveRequest, metadata bool) (any, error) {
	if len(r.ExternalID) != 32 || r.ExpectedVersion == "" {
		return nil, fmt.Errorf("ACCESS_DENIED: invalid resolve request")
	}
	var state pluginproto.StateValue
	if err := w.peer.Call(ctx, "host.state.get", pluginproto.StateRequest{Key: "track-" + r.ExternalID}, &state); err != nil {
		return nil, err
	}
	if len(state.Value) == 0 {
		return nil, fmt.Errorf("NOT_FOUND: source not found")
	}
	var item pluginproto.Observation
	if err := json.Unmarshal(state.Value, &item); err != nil {
		return nil, err
	}
	if item.ObservedVersion != r.ExpectedVersion {
		return nil, fmt.Errorf("RESOURCE_CHANGED: observation changed")
	}
	if metadata {
		return item, nil
	}
	var resource pluginproto.Resource
	err := w.peer.Call(ctx, "host.resource", pluginproto.Resource{RootID: item.RootID, RelativePath: item.RelativePath, ObservedVersion: item.ObservedVersion}, &resource)
	return resource, err
}
