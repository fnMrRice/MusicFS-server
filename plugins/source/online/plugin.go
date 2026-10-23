// Package onlineplugins composes the separately built official online plugins.
// Applications communicate with the executables through the SDK protocol.
package onlineplugins

import (
	"context"
	"io"
	"net/http"

	"github.com/fnMrRice/MusicFS-server/plugins/source/online/applemusic"
	"github.com/fnMrRice/MusicFS-server/plugins/source/online/bilibili"
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

// Plugin selects a declaration before opening the protocol channel.
func Plugin(platform string, options Options) provider.Plugin {
	switch platform {
	case online.Netease:
		return netease.Plugin(options)
	case online.QQMusic:
		return qqmusic.Plugin(options)
	case online.AppleMusic:
		return applemusic.Plugin(options)
	case online.Bilibili:
		return bilibili.Plugin(options)
	default:
		return provider.Plugin{}
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
