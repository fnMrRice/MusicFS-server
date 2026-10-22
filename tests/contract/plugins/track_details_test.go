package plugins_test

import (
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestProviderTrackDetailsContract(t *testing.T) {
	details := pluginproto.TrackDetails{ExternalID: "42", Title: "来源详情", Sections: []pluginproto.TrackDetailSection{{ID: "file", Title: "文件信息", Fields: []pluginproto.TrackDetailField{{ID: "name", Label: "文件名", Value: "测试.wav"}}}}}
	testkit.ValidatePlugin(t, "TrackDetails", details)
	request := map[string]any{"jsonrpc": "2.0", "id": "h-1", "method": "provider.trackDetails", "params": pluginproto.ResolveRequest{ExternalID: "42", ExpectedVersion: "version"}}
	testkit.ValidatePlugin(t, "Request", request)
	testkit.ValidatePlugin(t, "Success", map[string]any{"jsonrpc": "2.0", "id": "h-1", "result": details})
	scope := pluginproto.Context{InstanceID: "1463118e-e2d1-564a-ab70-05a357c1b9bf", UserID: "owner", TaskID: strings.Repeat("a", 48), TimeoutMs: 1000}
	onlineContract(t, "", map[string]any{"jsonrpc": "2.0", "id": "h-2", "method": "provider.online.trackDetails", "params": pluginproto.OnlineReference{Context: scope, ExternalID: "42"}})
	onlineContract(t, "", map[string]any{"jsonrpc": "2.0", "id": "h-2", "result": pluginproto.OnlineResult[pluginproto.TrackDetails]{Context: scope, Value: details}})
	for _, invalid := range []any{
		map[string]any{"externalId": "42", "title": "Track", "sections": nil},
		map[string]any{"externalId": "42", "title": "Track", "sections": []any{}, "html": "<script>"},
	} {
		if err := testkit.ContractSchema(t, "plugin", "TrackDetails").Validate(invalid); err == nil {
			t.Fatal("invalid provider detail schema was accepted", invalid)
		}
	}
}
