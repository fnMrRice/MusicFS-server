// Synthetic authenticated process used only to test startup task recovery.
package main

import (
	"context"
	"encoding/json"
	"os"
	"runtime"

	onlineplugins "github.com/fnMrRice/MusicFS-server/plugins/source/online"
	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
)

func main() {
	var hello pluginproto.OnlineHello
	peer := pluginproto.NewPeer(context.Background(), os.Stdin, os.Stdout, "w-", func(ctx context.Context, method string, raw json.RawMessage) (any, error) {
		if method == "host.hello" {
			if err := pluginproto.DecodeOnline(raw, &hello); err != nil {
				return nil, err
			}
			return pluginproto.Ready{InstanceID: hello.InstanceID, Nonce: hello.Nonce, Manifest: pluginproto.Manifest{PluginID: "musicfs.plugin.source.netease", Plugin: onlineplugins.Describe(online.Netease).Plugin, SupportsMultipleInstances: true, PluginVersion: "0.1.0", ProtocolVersion: "1.0", SourceType: "network", Roles: []string{"provider"}, Capabilities: pluginproto.OnlineCapabilities(online.Netease), Systems: []string{runtime.GOOS}, Architectures: []string{runtime.GOARCH}, MaxMessageBytes: pluginproto.MaxMessageBytes, MaxPageItems: online.MaxPageItems}}, nil
		}
		var scope pluginproto.OnlineScope
		if err := json.Unmarshal(raw, &scope); err != nil {
			return nil, err
		}
		if err := hello.ValidateScope(scope.Context); err != nil {
			return nil, err
		}
		switch method {
		case "provider.online.describe":
			return pluginproto.OnlineResult[pluginproto.OnlineDescriptor]{Context: scope.Context, Value: onlineplugins.Describe(online.Netease)}, nil
		case "provider.online.account":
			return pluginproto.OnlineResult[online.AccountResult]{Context: scope.Context, Value: online.AccountResult{View: online.AccountView{State: "signedIn", AccountID: "fixture-account", Revision: "fixture-revision"}}}, nil
		case "provider.online.playlist":
			<-ctx.Done()
			return nil, ctx.Err()
		default:
			return nil, pluginproto.Failure("UNSUPPORTED", "Unsupported lifecycle fixture method")
		}
	})
	<-peer.Done()
}
