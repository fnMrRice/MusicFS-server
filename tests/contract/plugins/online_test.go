package plugins_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/online"
	"github.com/fnMrRice/MusicFS-server/src/pluginproto"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

func onlineSchema(t *testing.T, name string) *jsonschema.Schema {
	t.Helper()
	c := jsonschema.NewCompiler()
	c.AssertFormat()
	const uri = "https://musicfs.invalid/contracts/plugins/v1/online.schema.json"
	if err := c.AddResource(uri, testkit.ReadJSON(t, "docs/contracts/plugins/v1/online.schema.json")); err != nil {
		t.Fatal(err)
	}
	location := uri
	if name != "" {
		location += "#/$defs/" + name
	}
	schema, err := c.Compile(location)
	if err != nil {
		t.Fatal(err)
	}
	return schema
}
func onlineContract(t *testing.T, name string, value any) {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err = onlineSchema(t, name).Validate(testkit.DecodeJSON(t, b)); err != nil {
		t.Fatalf("online %s contract: %v", name, err)
	}
}

type onlineResponse func(*http.Request) (*http.Response, error)

func (f onlineResponse) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestOnlineWireContractActualDTOs(t *testing.T) {
	scope := pluginproto.Context{InstanceID: "instance", UserID: "owner", TaskID: strings.Repeat("a", 48), TimeoutMs: 10000}
	hello := pluginproto.OnlineHello{Hello: pluginproto.Hello{ProtocolVersion: "1.0", InstanceID: scope.InstanceID, Nonce: strings.Repeat("b", 48), ConfigRevision: pluginproto.OnlineRevision}, UserID: scope.UserID, TimeoutMs: 10000}
	onlineContract(t, "Hello", hello)
	onlineContract(t, "", map[string]any{"jsonrpc": "2.0", "id": "h-1", "method": "host.hello", "params": hello})
	for _, platform := range []string{online.Netease, online.QQMusic, online.AppleMusic} {
		descriptor := pluginproto.DescribeOnline(platform)
		onlineContract(t, "Descriptor", descriptor)
		onlineContract(t, "", map[string]any{"jsonrpc": "2.0", "id": "h-2", "result": pluginproto.OnlineResult[pluginproto.OnlineDescriptor]{Context: scope, Value: descriptor}})
		manifest := pluginproto.Manifest{PluginID: pluginproto.OnlinePluginID(platform), PluginVersion: "0.1.0", ProtocolVersion: "1.0", Roles: []string{"provider"}, Capabilities: pluginproto.OnlineCapabilities(platform), Systems: []string{"windows", "linux"}, Architectures: []string{"amd64"}, MaxMessageBytes: pluginproto.MaxMessageBytes, MaxPageItems: online.MaxPageItems}
		onlineContract(t, "Ready", pluginproto.Ready{Manifest: manifest, InstanceID: scope.InstanceID, Nonce: hello.Nonce})
	}
	search := pluginproto.OnlineSearch{Context: scope, Query: "Synthetic", Limit: 1, Cursor: ""}
	onlineContract(t, "Search", search)
	fixture := testkit.ReadJSON(t, "docs/contracts/plugins/v1/fixtures/online-search.json")
	if err := onlineSchema(t, "").Validate(fixture); err != nil {
		t.Fatal(err)
	}
	c, err := online.New(online.Netease, online.Scope{UserID: "owner", InstanceID: "instance"}, online.Options{Transport: testkit.OnlineAccountTransport(onlineResponse(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"code":200,"result":{"songs":[{"id":42,"name":"Synthetic","artists":[],"album":{},"duration":1}],"songCount":1,"hasMore":false}}`))}, nil
	}))})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	testkit.RestoreOnlineFixture(t, c, online.Netease, "owner", "instance")
	page, err := c.Search(context.Background(), online.SearchRequest{Query: "Synthetic", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	onlineContract(t, "SearchPage", page)
	onlineContract(t, "", map[string]any{"jsonrpc": "2.0", "id": "h-3", "result": pluginproto.OnlineResult[online.SearchPage]{Context: scope, Value: page}})
	for _, code := range []string{"AUTH_REQUIRED", "PLATFORM_AUTH_REQUIRED", "TEMPORARY_FAILURE", "UNSUPPORTED"} {
		onlineContract(t, "", map[string]any{"jsonrpc": "2.0", "id": "h-4", "error": pluginproto.Failure(code, "Synthetic result")})
	}
	page.Tracks[0].SourceURL = "http://127.0.0.1/private"
	body, _ := json.Marshal(page)
	if onlineSchema(t, "SearchPage").Validate(testkit.DecodeJSON(t, body)) == nil {
		t.Fatal("untrusted source URL accepted by wire schema")
	}
}

func TestOnlineWireRejectsScopeAndUnverifiedCapabilities(t *testing.T) {
	base := func() map[string]any {
		return map[string]any{"instanceId": "instance", "userId": "owner", "taskId": strings.Repeat("a", 48), "timeoutMs": 10000}
	}
	for _, mutate := range []func(map[string]any){
		func(v map[string]any) { delete(v, "userId") }, func(v map[string]any) { v["accountRef"] = "private" }, func(v map[string]any) { v["taskId"] = "task" }, func(v map[string]any) { v["timeoutMs"] = 30001 }, func(v map[string]any) { v["cookie"] = "forbidden" },
	} {
		value := base()
		mutate(value)
		if onlineSchema(t, "Context").Validate(value) == nil {
			t.Fatal("unsafe online context accepted")
		}
	}
	descriptor := pluginproto.DescribeOnline(online.AppleMusic)
	descriptor.Capabilities[0].Supported = true
	if descriptor.Validate(online.AppleMusic) == nil {
		t.Fatal("unverified Apple capability accepted by host")
	}
	manifest := pluginproto.Manifest{PluginID: "musicfs.qqmusic", PluginVersion: "0.1.0", ProtocolVersion: "1.0", Roles: []string{"provider"}, Capabilities: []string{"stream.resolve"}, Systems: []string{"windows"}, Architectures: []string{"amd64"}, MaxMessageBytes: pluginproto.MaxMessageBytes, MaxPageItems: online.MaxPageItems}
	body, _ := json.Marshal(manifest)
	if onlineSchema(t, "Manifest").Validate(testkit.DecodeJSON(t, body)) == nil || pluginproto.ValidateOnlineManifest(manifest, online.QQMusic) == nil {
		t.Fatal("unverified audio capability accepted")
	}
}
