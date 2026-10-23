// Package onlineplugins 装配在线插件，应用通过 SDK 协议与独立实现通信。
package onlineplugins

import (
	"context"
	"io"
	"net/http"

	"github.com/fnMrRice/MusicFS-server/plugins/source/online/applemusic"
	"github.com/fnMrRice/MusicFS-server/plugins/source/online/internal/adapter"
	"github.com/fnMrRice/MusicFS-server/plugins/source/online/netease"
	"github.com/fnMrRice/MusicFS-server/plugins/source/online/qqmusic"
	"github.com/fnMrRice/MusicFS-server/sdk/fault"
	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/sdk/provider"
)

type Client = adapter.Client
type Options = adapter.Options

// New is a trusted composition helper, primarily for adapter integration tests.
func New(platform string, scope online.Scope, options Options) (*Client, error) {
	switch platform {
	case online.Netease:
		return netease.New(scope, options)
	case online.QQMusic:
		return qqmusic.New(scope, options)
	case online.AppleMusic:
		return applemusic.New(scope, options)
	default:
		return nil, fault.Fail("INVALID_ARGUMENT", "Unknown online platform")
	}
}

func Describe(platform string) pluginproto.OnlineDescriptor {
	return Plugin(platform, Options{}).Descriptor
}

func Run(ctx context.Context, platform string, input io.ReadCloser, output io.WriteCloser) error {
	return Serve(ctx, platform, input, output, nil)
}

// Serve allows test-only transport injection. RPC cannot select a transport.
func Serve(ctx context.Context, platform string, input io.ReadCloser, output io.WriteCloser, transport http.RoundTripper) error {
	return provider.Run(ctx, Plugin(platform, Options{Transport: transport}), input, output)
}
