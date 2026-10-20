package unified_test

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/plugins/source/online/unified"
	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/sdk/provider"
)

func TestSelectRejectsAmbiguousOrUnsupportedArguments(t *testing.T) {
	for _, args := range [][]string{
		nil, {"netease"}, {"--plugin"}, {"--plugin="}, {"--plugin", ""},
		{"--plugin", "local"}, {"--plugin", "NETEASE"}, {"--unknown", "netease"},
		{"--plugin", "netease", "extra"}, {"--plugin=qqmusic", "extra"},
		{"--plugin", "netease", "--plugin", "qqmusic"}, {"--", "--plugin", "netease"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			if _, err := unified.Select(args); err == nil {
				t.Fatal("invalid selection was accepted")
			}
		})
	}
}

func TestSelectionKeepsPlatformAndHostScopeForTheWholeSession(t *testing.T) {
	for _, platform := range []string{online.Netease, online.QQMusic, online.AppleMusic} {
		t.Run(platform, func(t *testing.T) {
			plugin, err := unified.Select([]string{"--plugin", platform})
			if err != nil {
				t.Fatal(err)
			}
			equalForm, err := unified.Select([]string{"--plugin=" + platform})
			if err != nil || equalForm.Descriptor.Platform != platform {
				t.Fatal("equals-form selection failed", err)
			}
			host, worker := net.Pipe()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			done := make(chan struct{})
			go func() {
				_ = provider.Run(ctx, plugin, worker, worker)
				close(done)
			}()
			peer := pluginproto.NewPeer(ctx, host, host, "h-", nil)
			defer func() {
				peer.Close()
				cancel()
				select {
				case <-done:
				case <-time.After(time.Second):
					t.Error("selected worker did not stop")
				}
			}()
			hello := pluginproto.OnlineHello{
				Hello: pluginproto.Hello{
					ProtocolVersion: pluginproto.ProtocolVersion, ConfigRevision: pluginproto.OnlineRevision,
					InstanceID: "1463118e-e2d1-564a-ab70-05a357c1b9bf", Nonce: strings.Repeat("a", 48),
				},
				UserID: "owner", TimeoutMs: 1000,
			}
			var ready pluginproto.Ready
			if err = peer.Call(ctx, "host.hello", hello, &ready); err != nil {
				t.Fatal(err)
			}
			if ready.Nonce != hello.Nonce || ready.InstanceID != hello.InstanceID {
				t.Fatal("selected worker lost the host handshake binding")
			}
			if err = pluginproto.ValidateOnlineManifest(ready.Manifest, platform); err != nil {
				t.Fatal(err)
			}
			scope := pluginproto.Context{
				UserID: hello.UserID, InstanceID: hello.InstanceID,
				TaskID: strings.Repeat("b", 48), TimeoutMs: 1000,
			}
			var descriptor pluginproto.OnlineResult[pluginproto.OnlineDescriptor]
			if err = peer.Call(ctx, "provider.online.describe", pluginproto.OnlineScope{Context: scope}, &descriptor); err != nil {
				t.Fatal(err)
			}
			if descriptor.Value.Platform != platform || descriptor.Context != scope {
				t.Fatal("selected worker changed platform or scope")
			}
			scope.UserID = "another-owner"
			err = peer.Call(ctx, "provider.online.describe", pluginproto.OnlineScope{Context: scope}, nil)
			var failure *pluginproto.RPCError
			if !errors.As(err, &failure) || failure.Data.Code != "ACCESS_DENIED" {
				t.Fatal("selected worker accepted another owner's scope", err)
			}
		})
	}
}
