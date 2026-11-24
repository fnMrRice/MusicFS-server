package plugins_test

import (
	"encoding/json"
	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/sdk/plugindisplay"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
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
	scope := pluginproto.Context{UserID: "owner", InstanceID: "a5e47aac-f949-5e45-9709-f28a0d8443df", TaskID: strings.Repeat("a", 48), TimeoutMs: 10000}
	binding := strings.Repeat("7a", 32)
	values := []any{
		map[string]any{"jsonrpc": "2.0", "id": "request", "method": "provider.online.account", "params": pluginproto.OnlineAccount{Context: scope, Request: online.AccountRequest{Action: "begin"}}},
		map[string]any{"jsonrpc": "2.0", "id": "request", "result": pluginproto.OnlineResult[online.AccountResult]{Context: scope, Value: online.AccountResult{View: online.AccountView{State: "signedOut"}}}},
		map[string]any{"jsonrpc": "2.0", "id": "request", "result": pluginproto.OnlineResult[online.AccountResult]{Context: scope, Value: online.AccountResult{View: online.AccountView{State: "signedIn", AccountID: "12345", DisplayName: "Fixture listener", AvatarURL: "https://p2.music.126.net/fixture/avatar.jpg"}}}},
		map[string]any{"jsonrpc": "2.0", "id": "request", "result": pluginproto.OnlineResult[online.AccountResult]{Context: scope, Value: online.AccountResult{View: online.AccountView{State: "signedIn", AccountID: "12345", Revision: "verified-1"}, RecommendationBinding: binding}}},
		map[string]any{"jsonrpc": "2.0", "id": "request", "result": pluginproto.OnlineResult[online.AccountResult]{Context: scope, Value: online.AccountResult{View: online.AccountView{State: "signedOut"}, DisplayCount: &plugindisplay.Count{Value: 0, Unit: "users", LocalizedUnits: map[string]string{"zh": "个用户"}}}}},
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
	if public.Validate(map[string]any{"state": "signedIn", "recommendationBinding": binding}) == nil {
		t.Fatal("public schema accepted a private recommendation binding")
	}
	for _, tier := range []string{"free", "vip", "svip"} {
		if err := public.Validate(map[string]any{"state": "signedIn", "revision": "login-1", "accountId": "12345", "membershipTier": tier}); err != nil {
			t.Fatal("valid membership rejected", err)
		}
	}
	for _, view := range []map[string]any{
		{"state": "unverified", "revision": "login-1", "accountId": "12345", "membershipTier": "svip"},
		{"state": "signedIn", "membershipTier": "vip"},
		{"state": "signedIn", "revision": "login-1", "accountId": "12345", "membershipTier": "future"},
	} {
		if public.Validate(view) == nil {
			t.Fatal("invalid membership accepted", view)
		}
	}
	// 通用契约只限定 URL 结构，平台主机约束由插件负责。
	if err := public.Validate(map[string]any{"state": "signedIn", "avatarUrl": "https://avatars.example.com/avatar.jpg"}); err != nil {
		t.Fatal("Public schema rejected a plugin-owned avatar host", err)
	}
	for _, address := range []string{"http://p1.music.126.net/avatar.jpg", "https://user@p1.music.126.net/avatar.jpg", "https://p1.music.126.net/avatar.jpg?token=private", "https://p1.music.126.net:443/avatar.jpg", "https://p1.music.126.net/avatar.jpg#private"} {
		if public.Validate(map[string]any{"state": "signedIn", "avatarUrl": address}) == nil {
			t.Fatal("Public schema accepted an unsupported avatar URL")
		}
	}
}
