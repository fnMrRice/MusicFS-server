package provider_test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/fault"
	"github.com/fnMrRice/MusicFS-server/sdk/instanceid"
	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/sdk/provider"
)

type detailsClient struct{ testClient }

func (*detailsClient) TrackDetails(_ context.Context, id string) (pluginproto.TrackDetails, error) {
	if id == "mismatch" {
		id = "other"
	}
	return pluginproto.TrackDetails{ExternalID: id, Title: "Plugin title", Sections: []pluginproto.TrackDetailSection{}}, nil
}

func TestTrackDetailsWorkerValidatesCapabilityScopeAndResult(t *testing.T) {
	for _, supported := range []bool{true, false} {
		t.Run(map[bool]string{true: "enabled", false: "unsupported"}[supported], func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			client := &detailsClient{}
			client.accountView.Store(&online.AccountView{State: "signedIn", AccountID: "account", Revision: "revision"})
			plugin := fixturePlugin(client)
			plugin.Descriptor.Capabilities = []online.Capability{{Name: "track.details", Supported: supported, Reason: "Plugin details"}}
			left, right := net.Pipe()
			done := make(chan error, 1)
			go func() { done <- provider.Run(ctx, plugin, right, right) }()
			host := pluginproto.NewPeer(ctx, left, left, "h-", nil)
			defer func() { host.Close(); <-done }()
			hello := pluginproto.OnlineHello{Hello: pluginproto.Hello{ProtocolVersion: pluginproto.ProtocolVersion, InstanceID: instanceid.Canonical("detail-test"), Nonce: fault.NewID(), ConfigRevision: pluginproto.OnlineRevision}, UserID: "owner", TimeoutMs: 3000}
			if err := host.Call(ctx, "host.hello", hello, nil); err != nil {
				t.Fatal(err)
			}
			scope := pluginproto.Context{InstanceID: hello.InstanceID, UserID: hello.UserID, TaskID: fault.NewID(), TimeoutMs: 1000}
			var result pluginproto.OnlineResult[pluginproto.TrackDetails]
			err := host.Call(ctx, "provider.online.trackDetails", pluginproto.OnlineReference{Context: scope, ExternalID: "42"}, &result)
			if !supported {
				if pluginproto.ErrorCode(err) != "UNSUPPORTED" {
					t.Fatal(err)
				}
				return
			}
			if err != nil || result.Context != scope || result.Value.Title != "Plugin title" {
				t.Fatal(result, err)
			}
			if err = host.Call(ctx, "provider.online.trackDetails", pluginproto.OnlineReference{Context: scope, ExternalID: "mismatch"}, nil); pluginproto.ErrorCode(err) != "PROTOCOL_MISMATCH" {
				t.Fatal("mismatched detail result was accepted", err)
			}
			scope.UserID = "another-user"
			if err = host.Call(ctx, "provider.online.trackDetails", pluginproto.OnlineReference{Context: scope, ExternalID: "42"}, nil); pluginproto.ErrorCode(err) != "ACCESS_DENIED" {
				t.Fatal("cross-user detail request was accepted", err)
			}
		})
	}
}
