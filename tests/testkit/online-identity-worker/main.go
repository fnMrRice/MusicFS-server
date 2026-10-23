// Synthetic identities exercise worker-to-host metadata flow without upstream IO.
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
	mode := "custom"
	if len(os.Args) > 1 {
		mode = os.Args[1]
	}
	descriptor := onlineplugins.Describe(online.Netease)
	descriptor.Plugin.Name = "Fixture Music"
	descriptor.Plugin.LocalizedNames = map[string]string{"zh-Hans": "测试音乐", "fr": "Musique de test"}
	switch mode {
	case "bad-id":
		descriptor.Plugin.ID = "musicfs.plugin.source.qqmusic"
	case "bad-name":
		descriptor.Plugin.Name = "Invalid\nName"
	case "bad-locale":
		descriptor.Plugin.LocalizedNames["zh_CN"] = "Invalid"
	case "stale-revision":
		descriptor.SchemaRevision = "online-authenticated-v2"
	case "missing-plugin":
		descriptor.Plugin = nil
	case "missing-source-feature":
		descriptor.Plugin.Features = []string{}
	case "capability-subset", "capability-mismatch":
		descriptor.Capabilities = []online.Capability{{Name: "metadata.track", Supported: true, Reason: "Fixture capability"}}
	}
	var hello pluginproto.OnlineHello
	peer := pluginproto.NewPeer(context.Background(), os.Stdin, os.Stdout, "w-", func(_ context.Context, method string, raw json.RawMessage) (any, error) {
		if method == "host.hello" {
			if err := pluginproto.DecodeOnline(raw, &hello); err != nil {
				return nil, err
			}
			manifest := pluginproto.Manifest{PluginID: "musicfs.plugin.source.netease", Plugin: descriptor.Plugin, SupportsMultipleInstances: true, PluginVersion: "0.1.0", ProtocolVersion: pluginproto.ProtocolVersion, SourceType: "network", Roles: []string{"provider"}, Capabilities: pluginproto.OnlineCapabilities(onlineplugins.Describe(online.Netease)), Systems: []string{runtime.GOOS}, Architectures: []string{runtime.GOARCH}, MaxMessageBytes: pluginproto.MaxMessageBytes, MaxPageItems: online.MaxPageItems}
			if mode == "changed-display" {
				manifest.Plugin = descriptor.Plugin.Clone()
				manifest.Plugin.Name = "Different handshake name"
			}
			if mode == "wrong-multiplicity" {
				manifest.SupportsMultipleInstances = false
			}
			switch mode {
			case "new-release", "capability-subset", "capability-mismatch":
				manifest.PluginVersion = "2.3.4-rc.1+fixture"
			case "bad-version":
				manifest.PluginVersion = "2.3.4-01"
			case "bad-protocol":
				manifest.ProtocolVersion = "2.0"
			}
			if mode == "capability-subset" {
				manifest.Capabilities = []string{"metadata.track"}
			}
			return pluginproto.Ready{Manifest: manifest, InstanceID: hello.InstanceID, Nonce: hello.Nonce}, nil
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
			return pluginproto.OnlineResult[pluginproto.OnlineDescriptor]{Context: scope.Context, Value: descriptor}, nil
		case "provider.online.account":
			view := online.AccountView{State: "signedOut"}
			if mode == "capability-subset" {
				view = online.AccountView{State: "signedIn", AccountID: "fixture-account", Revision: "fixture-revision"}
			}
			return pluginproto.OnlineResult[online.AccountResult]{Context: scope.Context, Value: online.AccountResult{View: view}}, nil
		default:
			return nil, pluginproto.Failure("UNSUPPORTED", "Unsupported identity fixture method")
		}
	})
	<-peer.Done()
}
