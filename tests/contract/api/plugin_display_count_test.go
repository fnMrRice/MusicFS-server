package api_test

import (
	"encoding/json"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/plugindisplay"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestPluginDisplayCountContract(t *testing.T) {
	count := plugindisplay.Count{Value: 0, Unit: "sources", LocalizedUnits: map[string]string{"zh": "个来源"}}
	raw, err := json.Marshal(count)
	if err != nil {
		t.Fatal(err)
	}
	testkit.ValidateHTTP(t, "PluginDisplayCount", raw)
	testkit.ValidatePlugin(t, "PluginDisplayCount", count)
	testkit.ValidatePlugin(t, "DisplayCountRequest", pluginproto.DisplayCountRequest{InstanceID: "b1548e96-3081-5c9c-8376-bf290e369d51", RootIDs: []string{"root-1"}})
	for _, family := range []string{"http", "plugin"} {
		schema := testkit.ContractSchema(t, family, "PluginDisplayCount")
		for _, value := range []map[string]any{
			{"value": -1, "unit": "sources", "localizedUnits": map[string]any{}},
			{"value": 9007199254740992, "unit": "sources", "localizedUnits": map[string]any{}},
			{"value": 1, "unit": "", "localizedUnits": map[string]any{}},
			{"value": 1, "unit": "sources"},
			{"value": 1, "unit": "sources", "localizedUnits": nil},
			{"value": 1, "unit": "sources", "localizedUnits": map[string]any{}, "instanceCount": 1},
		} {
			if err := schema.Validate(value); err == nil {
				t.Fatalf("%s accepted invalid display count: %v", family, value)
			}
		}
	}
	request := testkit.ContractSchema(t, "plugin", "Request")
	if err := request.Validate(map[string]any{
		"jsonrpc": "2.0", "id": "h-count", "method": "plugin.displayCount",
		"params": map[string]any{"instanceId": "b1548e96-3081-5c9c-8376-bf290e369d51", "rootIds": []any{"root-1"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := testkit.ContractSchema(t, "plugin", "DisplayCountRequest").Validate(map[string]any{
		"instanceId": "b1548e96-3081-5c9c-8376-bf290e369d51", "rootIds": []any{"root-1", "root-1"},
	}); err == nil {
		t.Fatal("accepted duplicate source identities")
	}
}
