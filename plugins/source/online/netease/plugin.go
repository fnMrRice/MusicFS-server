// Package netease declares the independently built NetEase Cloud Music plugin.
package netease

import (
	"time"

	"github.com/fnMrRice/MusicFS-server/plugins/source/online/internal/adapter"
	"github.com/fnMrRice/MusicFS-server/plugins/source/online/internal/policies"
	"github.com/fnMrRice/MusicFS-server/plugins/source/online/netease/driver"
	"github.com/fnMrRice/MusicFS-server/sdk/bundledplugin"
	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/sdk/provider"
)

type Options = adapter.Options

func Plugin(options Options) provider.Plugin {
	descriptor := pluginproto.OnlinePolicy(online.Netease)
	descriptor.Resources = policies.Resources(online.Netease)
	descriptor.Capabilities = policies.Capabilities(online.Netease)
	descriptor.Login = &online.LoginDescriptor{Method: "qr", Validation: "liveAccountPending"}
	descriptor.Plugin = &pluginproto.PluginIdentity{
		ID: "musicfs.plugin.source.netease", Name: "NetEase Cloud Music",
		LocalizedNames: map[string]string{"zh": "网易云音乐", "zh-Hans": "网易云音乐", "zh-Hant": "網易雲音樂"},
		Features:       []string{"music-source"},
	}
	return provider.Plugin{
		Descriptor: descriptor, Version: bundledplugin.NetEaseVersion,
		NewClient: func(scope online.Scope, timeout time.Duration) (provider.Client, error) {
			opts := options
			opts.Timeout = timeout
			return New(scope, opts)
		},
	}
}

func New(scope online.Scope, options Options) (*adapter.Client, error) {
	return adapter.New(online.Netease, scope, options, driver.New)
}
