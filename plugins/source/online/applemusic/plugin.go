// Package applemusic declares the independently built Apple Music login adapter.
package applemusic

import (
	"time"

	"github.com/fnMrRice/MusicFS-server/plugins/source/online/applemusic/driver"
	"github.com/fnMrRice/MusicFS-server/plugins/source/online/internal/adapter"
	"github.com/fnMrRice/MusicFS-server/sdk/bundledplugin"
	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/sdk/provider"
)

type Options = adapter.Options

func Plugin(options Options) provider.Plugin {
	descriptor := pluginproto.OnlinePolicy(online.AppleMusic)
	descriptor.Plugin = &pluginproto.PluginIdentity{
		ID: "musicfs.plugin.source.applemusic", Name: "Apple Music",
		LocalizedNames: map[string]string{}, Features: []string{"music-source"},
	}
	return provider.Plugin{
		Descriptor: descriptor, Version: bundledplugin.AppleMusicVersion,
		NewClient: func(scope online.Scope, timeout time.Duration) (provider.Client, error) {
			opts := options
			opts.Timeout = timeout
			return New(scope, opts)
		},
	}
}

func New(scope online.Scope, options Options) (*adapter.Client, error) {
	return adapter.New(online.AppleMusic, scope, options, driver.New)
}
