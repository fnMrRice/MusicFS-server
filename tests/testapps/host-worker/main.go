package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/fnMrRice/MusicFS-server/plugins/source/localfiles"
	onlineplugins "github.com/fnMrRice/MusicFS-server/plugins/source/online"
	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
)

type control struct {
	View    *online.AccountView `json:"view"`
	Count   json.RawMessage     `json:"count"`
	Failure string              `json:"failure"`
	Started string              `json:"started"`
	Release string              `json:"release"`
}

func main() {
	mode, dir := os.Args[1], os.Args[2]
	var peer *pluginproto.Peer
	var hello pluginproto.OnlineHello
	descriptor := onlineplugins.Describe(online.Netease)
	peer = pluginproto.NewPeer(context.Background(), os.Stdin, os.Stdout, "w-", func(ctx context.Context, method string, raw json.RawMessage) (any, error) {
		if method == "host.hello" {
			if err := json.Unmarshal(raw, &hello); err != nil {
				return nil, err
			}
			manifest := pluginproto.Manifest{PluginID: "musicfs.plugin.source.netease", Plugin: descriptor.Plugin,
				SupportsMultipleInstances: true, PluginVersion: "0.1.0", ProtocolVersion: pluginproto.ProtocolVersion,
				SourceType: "network", Roles: []string{"provider"}, Capabilities: pluginproto.OnlineCapabilities(online.Netease),
				Systems: []string{runtime.GOOS}, Architectures: []string{runtime.GOARCH}, MaxMessageBytes: pluginproto.MaxMessageBytes, MaxPageItems: online.MaxPageItems}
			if mode != "online" {
				manifest.PluginID, manifest.Plugin = "musicfs.plugin.source.localfiles", localfiles.Identity()
				manifest.SupportsMultipleInstances, manifest.Capabilities = false, nil
				manifest.MaxPageItems = pluginproto.MaxPageItems
			}
			return pluginproto.Ready{Manifest: manifest, InstanceID: hello.InstanceID, Nonce: hello.Nonce}, nil
		}
		if mode != "online" {
			return localCall(ctx, peer, mode, hello.InstanceID, method, raw)
		}
		var request pluginproto.OnlineAccount
		if err := json.Unmarshal(raw, &request); err != nil {
			return nil, err
		}
		if method == "provider.online.describe" {
			return pluginproto.OnlineResult[pluginproto.OnlineDescriptor]{Context: request.Context, Value: descriptor}, nil
		}
		if method != "provider.online.account" {
			return nil, pluginproto.Failure("UNSUPPORTED", "Unsupported fixture method")
		}
		if request.Request.Action == "signalCancel" {
			return pluginproto.OnlineResult[online.AccountResult]{Context: request.Context, Value: online.AccountResult{View: online.AccountView{State: "signedOut"}}}, nil
		}
		var c control
		body, _ := os.ReadFile(filepath.Join(dir, "control.json"))
		if len(body) > 0 {
			if err := json.Unmarshal(body, &c); err != nil {
				return nil, err
			}
		}
		if c.Started != "" {
			if err := os.WriteFile(c.Started, []byte("started"), 0600); err != nil {
				return nil, err
			}
		}
		for c.Release != "" {
			if _, err := os.Stat(c.Release); err == nil {
				break
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Millisecond):
			}
		}
		if c.Failure != "" {
			return nil, pluginproto.Failure(c.Failure, "Fixture account failure")
		}
		view := online.AccountView{State: "signedIn", AccountID: "12345", Revision: "login-1", DisplayName: "Platform username", LoginID: "private-login", QRURL: "https://example.invalid/login"}
		if c.View != nil {
			view = *c.View
		}
		count := c.Count
		if len(count) == 0 {
			count = json.RawMessage(`{"value":9,"unit":"custom units","localizedUnits":{"zh":"个自定义单位"}}`)
		}
		value := map[string]any{"view": view, "displayCount": count}
		return pluginproto.OnlineResult[any]{Context: request.Context, Value: value}, nil
	})
	<-peer.Done()
}
