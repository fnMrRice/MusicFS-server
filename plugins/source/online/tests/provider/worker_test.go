package provideronline_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	provideronline "github.com/fnMrRice/MusicFS-server/plugins/source/online"
	"github.com/fnMrRice/MusicFS-server/plugins/source/online/testkit"
	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func worker(t *testing.T, roundTripper http.RoundTripper) *pluginproto.Peer {
	t.Helper()
	client, server := net.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = provideronline.Serve(ctx, online.Netease, server, server, testkit.OnlineAccountTransport(roundTripper))
		close(done)
	}()
	peer := pluginproto.NewPeer(ctx, client, client, "h-", nil)
	t.Cleanup(func() {
		cancel()
		peer.Close()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("worker did not stop")
		}
	})
	return peer
}
func hello(t *testing.T, peer *pluginproto.Peer) pluginproto.OnlineHello {
	t.Helper()
	request := pluginproto.OnlineHello{Hello: pluginproto.Hello{ProtocolVersion: "1.0", InstanceID: "1463118e-e2d1-564a-ab70-05a357c1b9bf", Nonce: strings.Repeat("a", 48), ConfigRevision: pluginproto.OnlineRevision}, UserID: "owner", TimeoutMs: 3000}
	var ready pluginproto.Ready
	if err := peer.Call(context.Background(), "host.hello", request, &ready); err != nil {
		t.Fatal(err)
	}
	if ready.Nonce != request.Nonce || pluginproto.ValidateOnlineManifest(ready.Manifest, online.Netease) != nil {
		t.Fatal("manifest mismatch")
	}
	return request
}
func scope() pluginproto.Context {
	return pluginproto.Context{UserID: "owner", InstanceID: "1463118e-e2d1-564a-ab70-05a357c1b9bf", TaskID: strings.Repeat("b", 48), TimeoutMs: 3000}
}
func assertCode(t *testing.T, err error, code string) {
	t.Helper()
	var rpc *pluginproto.RPCError
	if !errors.As(err, &rpc) || rpc.Data.Code != code {
		t.Fatalf("expected %s, got %v", code, err)
	}
}

func TestOnlineWorkerRequiresHandshakeAndExactScope(t *testing.T) {
	var calls atomic.Int32
	peer := worker(t, transport(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, errors.New("unexpected request")
	}))
	request := pluginproto.OnlineSearch{Context: scope(), Query: "synthetic", Limit: 1}
	assertCode(t, peer.Call(context.Background(), "provider.online.search", request, nil), "PROTOCOL_MISMATCH")
	h := hello(t, peer)
	assertCode(t, peer.Call(context.Background(), "host.hello", h, nil), "PROTOCOL_MISMATCH")
	for _, mutate := range []func(*pluginproto.Context){
		func(c *pluginproto.Context) { c.UserID = "other" }, func(c *pluginproto.Context) { c.InstanceID = "other" }, func(c *pluginproto.Context) { c.AccountRef = "private" }, func(c *pluginproto.Context) { c.TimeoutMs = 3001 }, func(c *pluginproto.Context) { c.TaskID = "unbound" },
	} {
		r := request
		mutate(&r.Context)
		assertCode(t, peer.Call(context.Background(), "provider.online.search", r, nil), "ACCESS_DENIED")
	}
	assertCode(t, peer.Call(context.Background(), "provider.online.search", map[string]any{"context": scope(), "query": "synthetic", "limit": 1, "cursor": "", "url": "http://127.0.0.1/private"}, nil), "PROTOCOL_MISMATCH")
	if calls.Load() != 0 {
		t.Fatal("unauthorized scope reached the upstream transport")
	}
}

func TestOnlineWorkerCancellationReachesTransport(t *testing.T) {
	started, stopped := make(chan struct{}), make(chan struct{})
	peer := worker(t, transport(func(r *http.Request) (*http.Response, error) {
		close(started)
		<-r.Context().Done()
		close(stopped)
		return nil, r.Context().Err()
	}))
	hello(t, peer)
	authenticate(t, peer)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- peer.Call(ctx, "provider.online.search", pluginproto.OnlineSearch{Context: scope(), Query: "slow", Limit: 1}, nil)
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("request did not start")
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("cancellation did not reach HTTP transport")
	}
	var descriptor pluginproto.OnlineResult[pluginproto.OnlineDescriptor]
	if err := peer.Call(context.Background(), "provider.online.describe", pluginproto.OnlineScope{Context: scope()}, &descriptor); err != nil {
		t.Fatal(err)
	}
}

func TestOnlineWorkerOversizedResultDoesNotCorruptControlChannel(t *testing.T) {
	artists := make([]map[string]string, 128)
	for i := range artists {
		artists[i] = map[string]string{"name": strings.Repeat("x", 4096)}
	}
	song := map[string]any{"id": 42, "name": "Synthetic", "artists": artists, "album": map[string]string{"name": "Album"}, "duration": 1}
	body, _ := json.Marshal(map[string]any{"code": 200, "result": map[string]any{"songs": []any{song, song}, "songCount": 2, "hasMore": false}})
	peer := worker(t, transport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(body)))}, nil
	}))
	hello(t, peer)
	authenticate(t, peer)
	assertCode(t, peer.Call(context.Background(), "provider.online.search", pluginproto.OnlineSearch{Context: scope(), Query: "large", Limit: 2}, nil), "UNSUPPORTED")
	var descriptor pluginproto.OnlineResult[pluginproto.OnlineDescriptor]
	if err := peer.Call(context.Background(), "provider.online.describe", pluginproto.OnlineScope{Context: scope()}, &descriptor); err != nil {
		t.Fatalf("oversized result killed peer: %v", err)
	}
}

func authenticate(t *testing.T, peer *pluginproto.Peer) {
	t.Helper()
	secret := json.RawMessage(`{"platform":"netease","userId":"owner","instanceId":"1463118e-e2d1-564a-ab70-05a357c1b9bf","accountId":"12345","deviceId":"fixture","cookies":{"MUSIC_U":"fixture-private-token"}}`)
	for _, r := range []online.AccountRequest{{Action: "restore", Secret: secret}, {Action: "check"}} {
		var result pluginproto.OnlineResult[online.AccountResult]
		if err := peer.Call(context.Background(), "provider.online.account", pluginproto.OnlineAccount{Context: scope(), Request: r}, &result); err != nil {
			t.Fatal(err)
		}
	}
	time.Sleep(1010 * time.Millisecond)
}
