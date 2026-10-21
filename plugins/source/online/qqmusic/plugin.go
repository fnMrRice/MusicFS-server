// Package qqmusic declares the independently built QQ Music plugin.
package qqmusic

import (
	"time"

	"github.com/fnMrRice/MusicFS-server/plugins/source/online/internal/adapter"
	"github.com/fnMrRice/MusicFS-server/plugins/source/online/qqmusic/driver"
	"github.com/fnMrRice/MusicFS-server/sdk/bundledplugin"
	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/sdk/provider"
)

type Options = adapter.Options

func Plugin(options Options) provider.Plugin {
	descriptor := pluginproto.OnlinePolicy(online.QQMusic)
	descriptor.Plugin = &pluginproto.PluginIdentity{
		ID: "musicfs.plugin.source.qqmusic", Name: "QQ Music",
		LocalizedNames: map[string]string{"zh": "QQ 音乐", "zh-Hans": "QQ 音乐", "zh-Hant": "QQ 音樂"},
		Features:       []string{"music-source"},
	}
	return provider.Plugin{
		Descriptor: descriptor, Version: bundledplugin.QQMusicVersion,
		NewClient: func(scope online.Scope, timeout time.Duration) (provider.Client, error) {
			opts := options
			opts.Timeout = timeout
			return New(scope, opts)
		},
	}
}

func New(scope online.Scope, options Options) (*adapter.Client, error) {
	return adapter.New(online.QQMusic, scope, options, driver.New)
}
