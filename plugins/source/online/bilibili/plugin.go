// Package bilibili 提供匿名优先的 Bilibili 公开音频来源。
package bilibili

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/fnMrRice/MusicFS-server/plugins/source/online/internal/adapter"
	"github.com/fnMrRice/MusicFS-server/sdk/bundledplugin"
	"github.com/fnMrRice/MusicFS-server/sdk/fault"
	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/sdk/provider"
)

type Options = adapter.Options

type Client struct {
	provider.UnimplementedClient
	http      *http.Client
	mu        sync.Mutex
	snapshots map[string]snapshot
}

var _ provider.Client = (*Client)(nil)
var _ provider.AudioClient = (*Client)(nil)

func Plugin(options Options) provider.Plugin {
	d := pluginproto.OnlinePolicy(online.Bilibili)
	d.Plugin = &pluginproto.PluginIdentity{ID: "musicfs.plugin.source.bilibili", Name: "Bilibili", LocalizedNames: map[string]string{"zh": "哔哩哔哩", "zh-Hans": "哔哩哔哩", "zh-Hant": "嗶哩嗶哩"}, Features: []string{"music-source"}}
	return provider.Plugin{Descriptor: d, Version: bundledplugin.BilibiliVersion, NewClient: func(scope online.Scope, timeout time.Duration) (provider.Client, error) {
		opts := options
		opts.Timeout = timeout
		return New(scope, opts)
	}}
}

func New(scope online.Scope, options Options) (*Client, error) {
	for _, value := range []string{scope.UserID, scope.InstanceID} {
		clean, err := online.TextValue(value, 128)
		if err != nil || clean == "" || clean != value {
			return nil, fault.Fail("INVALID_ARGUMENT", "A valid host-owned online scope is required")
		}
	}
	if scope.AccountRef != "" {
		return nil, fault.Fail("UNSUPPORTED", "Bilibili account selection is not implemented")
	}
	if options.Timeout == 0 {
		options.Timeout = 10 * time.Second
	}
	if options.Timeout < time.Second || options.Timeout > 30*time.Second {
		return nil, fault.Fail("INVALID_ARGUMENT", "Online request timeout must be between 1 and 30 seconds")
	}
	if options.Transport == nil {
		options.Transport = publicTransport()
	}
	return &Client{http: &http.Client{Transport: options.Transport, Timeout: options.Timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, snapshots: map[string]snapshot{}}, nil
}

func (c *Client) Close() { c.http.CloseIdleConnections() }

func (c *Client) Account(ctx context.Context, r online.AccountRequest) (online.AccountResult, error) {
	if err := ctx.Err(); err != nil {
		return online.AccountResult{}, err
	}
	if r.Action != "state" && r.Action != "logout" {
		return online.AccountResult{}, fault.Fail("UNSUPPORTED", "Bilibili account login is not implemented")
	}
	return online.AccountResult{View: online.AccountView{State: "signedOut"}}, nil
}
