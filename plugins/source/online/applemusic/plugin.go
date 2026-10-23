// Package applemusic declares the independently built Apple Music login adapter.
package applemusic

import (
	"context"
	"os"
	"time"

	"github.com/fnMrRice/MusicFS-server/plugins/source/online/applemusic/driver"
	"github.com/fnMrRice/MusicFS-server/plugins/source/online/internal/adapter"
	"github.com/fnMrRice/MusicFS-server/plugins/source/online/internal/policies"
	"github.com/fnMrRice/MusicFS-server/sdk/bundledplugin"
	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/sdk/provider"
)

type Options = adapter.Options

func Plugin(options Options) provider.Plugin {
	descriptor := pluginproto.OnlinePolicy(online.AppleMusic)
	descriptor.Resources = policies.Resources(online.AppleMusic)
	descriptor.Capabilities = policies.Capabilities(online.AppleMusic)
	descriptor.Login = &online.LoginDescriptor{Method: "musicKit", Validation: "liveAccountPending"}
	descriptor.Plugin = &pluginproto.PluginIdentity{
		ID: "musicfs.plugin.source.applemusic", Name: "Apple Music",
		LocalizedNames: map[string]string{}, Features: []string{"music-source"},
	}
	return provider.Plugin{
		Environment: []string{"MUSICFS_APPLE_DEVELOPER_TOKEN"},
		Descriptor:  descriptor, Version: bundledplugin.AppleMusicVersion,
		NewClient: func(scope online.Scope, timeout time.Duration) (provider.Client, error) {
			opts := options
			opts.Timeout = timeout
			return New(scope, opts)
		},
	}
}

func New(scope online.Scope, options Options) (*adapter.Client, error) {
	// 保留既有 MusicKit 登录行为，只迁移平台适配责任；不返回私钥或用户令牌。
	options.LoginOptions = func(ctx context.Context) (online.AccountOptions, error) {
		token := os.Getenv("MUSICFS_APPLE_DEVELOPER_TOKEN")
		return online.AccountOptions{Method: "musicKit", DeveloperToken: token, Configured: token != "", Validation: "liveAccountPending"}, ctx.Err()
	}
	options.PrepareAccount = func(request online.AccountRequest) online.AccountRequest {
		if request.Action == "begin" && request.DeveloperToken == "" {
			request.DeveloperToken = os.Getenv("MUSICFS_APPLE_DEVELOPER_TOKEN")
		}
		return request
	}
	return adapter.New(online.AppleMusic, scope, options, driver.New)
}
