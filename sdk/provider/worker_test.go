package provider_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/fault"
	"github.com/fnMrRice/MusicFS-server/sdk/instanceid"
	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/sdk/provider"
)

type testClient struct {
	provider.UnimplementedClient
	searchCalls     atomic.Int32
	accountCalls    atomic.Int32
	accountView     atomic.Pointer[online.AccountView]
	closed          atomic.Bool
	metadataStarted chan struct{}
	metadataRelease chan struct{}
}

func (c *testClient) Close() { c.closed.Store(true) }
func (c *testClient) Account(context.Context, online.AccountRequest) (online.AccountResult, error) {
	c.accountCalls.Add(1)
	if view := c.accountView.Load(); view != nil {
		return online.AccountResult{View: *view}, nil
	}
	return online.AccountResult{View: online.AccountView{State: "signedOut"}}, nil
}
func (c *testClient) Metadata(ctx context.Context, id string) (online.Track, error) {
	if c.metadataStarted != nil && (id == "51" || id == "52") {
		c.metadataStarted <- struct{}{}
		select {
		case <-c.metadataRelease:
		case <-ctx.Done():
			return online.Track{}, ctx.Err()
		}
	}
	if id == "oversized" {
		return online.Track{ExternalID: id, Title: strings.Repeat("x", pluginproto.MaxMessageBytes)}, nil
	}
	if id == "failure" {
		delay := 3
		return online.Track{}, fmt.Errorf("adapter: %w", &fault.Error{Code: "RATE_LIMITED", Message: "Slow down", RetryAfter: &delay})
	}
	track := online.Track{ExternalID: id, Title: "Synthetic", Artists: []string{}, SourceURL: "https://music.163.com/song?id=" + id, PlaybackAvailability: "unknown"}
	track.MetadataDigest = online.Digest(track)
	return track, nil
}
func (c *testClient) Search(context.Context, online.SearchRequest) (online.SearchPage, error) {
	c.searchCalls.Add(1)
	return online.SearchPage{}, errors.New("disabled implementation was reached")
}

func fixturePlugin(client provider.Client) provider.Plugin {
	d := pluginproto.OnlinePolicy(online.Netease)
	d.Resources = online.ResourcePolicy{Tracks: []online.ReferenceRule{{IDPattern: `^([1-9][0-9]{0,19})$`, URLTemplate: "https://music.163.com/song?id=${1}"}}}
	d.Plugin = &pluginproto.PluginIdentity{
		ID: pluginproto.OnlinePluginID(online.Netease), Name: "Test metadata provider",
		LocalizedNames: map[string]string{}, Features: []string{"music-source"},
	}
	d.Capabilities = []online.Capability{{Name: "metadata.track", Supported: true, Reason: "Metadata only"}}
	return provider.Plugin{Descriptor: d, Version: "3.2.1", NewClient: func(online.Scope, time.Duration) (provider.Client, error) { return client, nil }}
}

func TestWorkerNegotiatesAndEnforcesCapabilitySubset(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	left, right := net.Pipe()
	client := &testClient{metadataStarted: make(chan struct{}, 2), metadataRelease: make(chan struct{})}
	client.accountView.Store(&online.AccountView{State: "signedIn", AccountID: "fixture-account", Revision: "fixture-revision"})
	finished := make(chan error, 1)
	go func() { finished <- provider.Run(ctx, fixturePlugin(client), right, right) }()
	host := pluginproto.NewPeer(ctx, left, left, "h-", nil)
	t.Cleanup(func() {
		host.Close()
		select {
		case <-finished:
			if !client.closed.Load() {
				t.Error("worker did not close the account client")
			}
		case <-ctx.Done():
			t.Error("worker did not stop after its control channel closed")
		}
	})
	hello := pluginproto.OnlineHello{
		Hello: pluginproto.Hello{
			ProtocolVersion: pluginproto.ProtocolVersion, InstanceID: instanceid.Canonical("sdk-test"),
			Nonce: fault.NewID(), ConfigRevision: pluginproto.OnlineRevision,
		},
		UserID: "fixture-user", TimeoutMs: 3000,
	}
	var ready pluginproto.Ready
	if err := host.Call(ctx, "host.hello", hello, &ready); err != nil {
		t.Fatal(err)
	}
	scope := pluginproto.Context{InstanceID: hello.InstanceID, UserID: hello.UserID, TaskID: fault.NewID(), TimeoutMs: 1000}
	var descriptor pluginproto.OnlineResult[pluginproto.OnlineDescriptor]
	if err := host.Call(ctx, "provider.online.describe", pluginproto.OnlineScope{Context: scope}, &descriptor); err != nil {
		t.Fatal(err)
	}
	if ready.Manifest.PluginVersion != "3.2.1" || pluginproto.ValidateOnlineDescriptor(ready.Manifest, descriptor.Value, online.Netease) != nil {
		t.Fatal("worker did not negotiate a compatible independent release")
	}
	var metadata pluginproto.OnlineResult[online.Track]
	if err := host.Call(ctx, "provider.online.metadata", pluginproto.OnlineReference{Context: scope, ExternalID: "42"}, &metadata); err != nil || online.ValidateTrack(descriptor.Value.Resources, metadata.Value) != nil {
		t.Fatalf("enabled metadata method failed: %v", err)
	}
	if client.accountCalls.Load() != 0 {
		t.Fatal("enabled capability made an unnecessary account-state request")
	}
	var account pluginproto.OnlineResult[online.AccountResult]
	if err := host.Call(ctx, "provider.online.account", pluginproto.OnlineAccount{Context: scope, Request: online.AccountRequest{Action: "state"}}, &account); err != nil || account.Value.View.State != "signedIn" {
		t.Fatalf("account control message was blocked by the business capability gate: %v", err)
	}
	t.Run("account reads are isolated from occupied metadata slots", func(t *testing.T) {
		done := make(chan error, 2)
		for _, id := range []string{"51", "52"} {
			go func() {
				done <- host.Call(ctx, "provider.online.metadata", pluginproto.OnlineReference{Context: scope, ExternalID: id}, nil)
			}()
		}
		defer func() {
			close(client.metadataRelease)
			for range 2 {
				if err := <-done; err != nil {
					t.Error("admitted metadata read failed", err)
				}
			}
		}()
		for range 2 {
			select {
			case <-client.metadataStarted:
			case <-ctx.Done():
				t.Fatal("metadata did not occupy both slots")
			}
		}
		if err := host.Call(ctx, "provider.online.metadata", pluginproto.OnlineReference{Context: scope, ExternalID: "53"}, nil); pluginproto.ErrorCode(err) != "RATE_LIMITED" {
			t.Fatal("upstream concurrency was not enforced", err)
		}
		if err := host.Call(ctx, "provider.online.account", pluginproto.OnlineAccount{Context: scope, Request: online.AccountRequest{Action: "state"}}, &account); err != nil || account.Value.View.State != "signedIn" {
			t.Fatal("local account read was blocked by metadata", err)
		}
		if err := host.Call(ctx, "provider.online.account", pluginproto.OnlineAccount{Context: scope, Request: online.AccountRequest{Action: "check"}}, &account); err != nil {
			t.Fatal("account verification was blocked by metadata", err)
		}
		if err := host.Call(ctx, "provider.online.accountOptions", pluginproto.OnlineScope{Context: scope}, nil); err != nil {
			t.Fatal("account options were blocked by metadata", err)
		}
	})
	t.Run("disabled search never reaches adapter", func(t *testing.T) {
		err := host.Call(ctx, "provider.online.search", pluginproto.OnlineSearch{Context: scope, Kind: "tracks", Query: "fixture", Limit: 1}, nil)
		if pluginproto.ErrorCode(err) != "UNSUPPORTED" || client.searchCalls.Load() != 0 {
			t.Fatalf("disabled capability reached adapter: %v", err)
		}
	})
	t.Run("authentication precedes unsupported capability", func(t *testing.T) {
		previous := client.accountView.Load()
		defer client.accountView.Store(previous)
		for name, view := range map[string]online.AccountView{
			"signed out":       {State: "signedOut"},
			"account missing":  {State: "signedIn", Revision: "fixture-revision"},
			"revision missing": {State: "signedIn", AccountID: "fixture-account"},
		} {
			t.Run(name, func(t *testing.T) {
				client.accountView.Store(&view)
				err := host.Call(ctx, "provider.online.search", pluginproto.OnlineSearch{Context: scope, Kind: "tracks", Query: "fixture", Limit: 1}, nil)
				if pluginproto.ErrorCode(err) != "PLATFORM_AUTH_REQUIRED" || client.searchCalls.Load() != 0 {
					t.Fatalf("unauthenticated request did not preserve login-first semantics: %v", err)
				}
			})
		}
	})
	t.Run("scope validated before capability", func(t *testing.T) {
		wrong := scope
		wrong.UserID = "another-user"
		err := host.Call(ctx, "provider.online.search", pluginproto.OnlineSearch{Context: wrong, Kind: "tracks", Query: "fixture", Limit: 1}, nil)
		if pluginproto.ErrorCode(err) != "ACCESS_DENIED" {
			t.Fatalf("cross-account request was not rejected: %v", err)
		}
	})
	t.Run("shared error crosses RPC", func(t *testing.T) {
		err := host.Call(ctx, "provider.online.metadata", pluginproto.OnlineReference{Context: scope, ExternalID: "failure"}, nil)
		var rpc *pluginproto.RPCError
		if !errors.As(err, &rpc) || rpc.Data.Code != "RATE_LIMITED" || rpc.Data.RetryAfterMs == nil || *rpc.Data.RetryAfterMs != 3000 {
			t.Fatalf("typed SDK failure lost code or retry delay: %v", err)
		}
	})
	t.Run("oversized result does not report transient throttling", func(t *testing.T) {
		err := host.Call(ctx, "provider.online.metadata", pluginproto.OnlineReference{Context: scope, ExternalID: "oversized"}, nil)
		var rpc *pluginproto.RPCError
		if !errors.As(err, &rpc) || rpc.Data.Code != "UNSUPPORTED" || rpc.Data.Retryable {
			t.Fatalf("oversized response must not be retried: %v", err)
		}
	})
	t.Run("second handshake rejected", func(t *testing.T) {
		if err := host.Call(ctx, "host.hello", hello, nil); pluginproto.ErrorCode(err) != "PROTOCOL_MISMATCH" {
			t.Fatalf("second handshake accepted: %v", err)
		}
	})
}

func TestPluginRejectsInvalidDeclarationBeforeOpeningChannel(t *testing.T) {
	plugin := fixturePlugin(&testClient{})
	plugin.Version = "development"
	if err := provider.Run(context.Background(), plugin, nil, nil); err == nil {
		t.Fatal("invalid release version opened the worker transport")
	}
	plugin = fixturePlugin(&testClient{})
	plugin.NewClient = nil
	if err := plugin.Validate(); err == nil {
		t.Fatal("nil client factory accepted")
	}
}
