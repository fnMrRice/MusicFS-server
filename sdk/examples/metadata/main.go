// This offline example uses synthetic data and never contacts a music service.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/fault"
	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/sdk/provider"
)

type client struct{ provider.UnimplementedClient }

func (client) Account(_ context.Context, request online.AccountRequest) (online.AccountResult, error) {
	if request.Action != "state" {
		return online.AccountResult{}, fault.Fail("UNSUPPORTED", "This offline example does not sign in")
	}
	return online.AccountResult{View: online.AccountView{State: "signedOut"}}, nil
}

func (client) Metadata(ctx context.Context, id string) (online.Track, error) {
	if err := ctx.Err(); err != nil {
		return online.Track{}, err
	}
	if id != "42" {
		return online.Track{}, fault.Fail("NOT_FOUND", "Only synthetic track 42 exists")
	}
	track := online.Track{
		ExternalID: "42", Title: "Synthetic example", Artists: []string{"Example artist"},
		SourceURL: "https://music.163.com/song?id=42", PlaybackAvailability: "unknown",
	}
	track.MetadataDigest = online.Digest(track)
	return track, nil
}

func main() {
	descriptor := pluginproto.OnlinePolicy(online.Netease)
	descriptor.Plugin = &pluginproto.PluginIdentity{
		ID: pluginproto.OnlinePluginID(online.Netease), Name: "Offline SDK example",
		LocalizedNames: map[string]string{"zh-CN": "离线 SDK 示例"}, Features: []string{"music-source"},
	}
	descriptor.Capabilities = []online.Capability{
		{Name: "metadata.track", Supported: true, Reason: "Synthetic metadata for SDK protocol testing"},
	}
	plugin := provider.Plugin{
		Descriptor: descriptor, Version: "1.0.0",
		NewClient: func(online.Scope, time.Duration) (provider.Client, error) { return client{}, nil },
	}
	if err := provider.Run(context.Background(), plugin, os.Stdin, os.Stdout); err != nil && !errors.Is(err, io.EOF) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
