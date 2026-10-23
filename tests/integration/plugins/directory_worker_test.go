package plugins_test

import (
	"context"
	"encoding/json"

	"github.com/fnMrRice/MusicFS-server/sdk/directoryidentity"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
)

// 独立 Provider 仅实现公开 SDK 协议，不导入 Local Files 的私有身份逻辑。
func registeredDirectoryCall(ctx context.Context, peer *pluginproto.Peer, method string, raw json.RawMessage) (any, error) {
	if method != "provider.observePackage" {
		return nil, pluginproto.Failure("UNSUPPORTED", "Unsupported test method")
	}
	var request pluginproto.PackageObserveRequest
	if err := json.Unmarshal(raw, &request); err != nil {
		return nil, err
	}
	result := pluginproto.PackageObserveResult{Items: []pluginproto.Observation{}}
	for _, entry := range request.Entries {
		item := pluginproto.Observation{ExternalID: "independent-track", SourceRef: "opaque:asset/7", RootID: entry.RootID, RelativePath: entry.RelativePath, ObservedVersion: entry.ObservedVersion, SizeBytes: entry.SizeBytes, MIME: entry.MIME, DisplayName: "Independent track"}
		values := map[string]any{
			directoryidentity.AliasKey(entry.RootID, entry.FileIdentity): directoryidentity.Aliases{Paths: map[string]string{entry.RelativePath: item.ExternalID}},
			directoryidentity.TrackKey(item.ExternalID):                  item,
		}
		for key, value := range values {
			var previous pluginproto.StateValue
			if err := peer.Call(ctx, "host.state.get", pluginproto.StateRequest{Key: key}, &previous); err != nil {
				return nil, err
			}
			encoded, err := json.Marshal(value)
			if err != nil {
				return nil, err
			}
			if err := peer.Call(ctx, "host.state.compareAndSet", pluginproto.StateRequest{Key: key, Revision: previous.Revision, Value: encoded}, nil); err != nil {
				return nil, err
			}
		}
		result.Items = append(result.Items, item)
	}
	return result, nil
}
