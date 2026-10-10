package plugins_test

import (
	"encoding/json"
	"github.com/fnMrRice/MusicFS-server/src/online"
	"github.com/fnMrRice/MusicFS-server/src/pluginproto"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"strings"
	"testing"
)

func TestPlatformAccountWireSchema(t *testing.T) {
	compiler := jsonschema.NewCompiler()
	for _, path := range []string{"plugins/v1/online.schema.json", "plugins/v1/account.schema.json", "platform-account.schema.json"} {
		if err := compiler.AddResource("https://musicfs.invalid/contracts/"+path, testkit.ReadJSON(t, "docs/contracts/"+path)); err != nil {
			t.Fatal(err)
		}
	}
	schema, err := compiler.Compile("https://musicfs.invalid/contracts/plugins/v1/account.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	scope := pluginproto.Context{UserID: "owner", InstanceID: "net", TaskID: strings.Repeat("a", 48), TimeoutMs: 10000}
	values := []any{
		map[string]any{"jsonrpc": "2.0", "id": "request", "method": "provider.online.account", "params": pluginproto.OnlineAccount{Context: scope, Request: online.AccountRequest{Action: "begin"}}},
		map[string]any{"jsonrpc": "2.0", "id": "request", "result": pluginproto.OnlineResult[online.AccountResult]{Context: scope, Value: online.AccountResult{View: online.AccountView{State: "signedOut"}}}},
	}
	for _, value := range values {
		raw, _ := json.Marshal(value)
		if err := schema.Validate(testkit.DecodeJSON(t, raw)); err != nil {
			t.Fatal(err)
		}
	}
	public, err := compiler.Compile("https://musicfs.invalid/contracts/platform-account.schema.json#/$defs/View")
	if err != nil {
		t.Fatal(err)
	}
	if public.Validate(map[string]any{"state": "signedIn", "secret": map[string]any{"token": "fixture"}}) == nil {
		t.Fatal("public schema accepted secret")
	}
}
