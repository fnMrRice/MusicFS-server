package provider_test

import (
	"bytes"
	"context"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/fault"
	"github.com/fnMrRice/MusicFS-server/sdk/instanceid"
	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/sdk/provider"
)

type audioFixtureClient struct {
	provider.UnimplementedClient
	reader io.ReadCloser
	opens  atomic.Int32
}

func (c *audioFixtureClient) DescribeAudio(_ context.Context, id string) (pluginproto.OnlineAudio, error) {
	return pluginproto.OnlineAudio{ExternalID: id, MIME: "audio/mp4", Codec: "aac", Container: "m4a", SizeBytes: -1}, nil
}
func (c *audioFixtureClient) OpenAudio(ctx context.Context, id string) (pluginproto.OnlineAudio, io.ReadCloser, error) {
	c.opens.Add(1)
	description, _ := c.DescribeAudio(ctx, id)
	return description, c.reader, nil
}

func TestAudioControlStreamIsLazyScopedAndSequential(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	reader, writer := io.Pipe()
	defer writer.Close()
	client := &audioFixtureClient{reader: reader}
	descriptor := pluginproto.OnlinePolicy(online.Bilibili)
	descriptor.Plugin = &pluginproto.PluginIdentity{ID: pluginproto.OnlinePluginID(online.Bilibili), Name: "Audio fixture", LocalizedNames: map[string]string{}, Features: []string{"music-source"}}
	descriptor.Capabilities = []online.Capability{{Name: "stream.resolve", Supported: true, Reason: "Fixture audio"}}
	plugin := provider.Plugin{Descriptor: descriptor, Version: "0.1.0", NewClient: func(online.Scope, time.Duration) (provider.Client, error) { return client, nil }}
	left, right := net.Pipe()
	defer left.Close()
	go provider.Run(ctx, plugin, right, right)
	host := pluginproto.NewPeer(ctx, left, left, "h-", nil)
	defer host.Close()
	hello := pluginproto.OnlineHello{Hello: pluginproto.Hello{ProtocolVersion: pluginproto.ProtocolVersion, InstanceID: instanceid.Canonical("network-audio"), Nonce: fault.NewID(), ConfigRevision: pluginproto.OnlineRevision}, UserID: "owner", TimeoutMs: 3000}
	var ready pluginproto.Ready
	if err := host.Call(ctx, "host.hello", hello, &ready); err != nil {
		t.Fatal(err)
	}
	scope := pluginproto.Context{UserID: hello.UserID, InstanceID: hello.InstanceID, TaskID: fault.NewID(), TimeoutMs: 1000}
	var description pluginproto.OnlineResult[pluginproto.OnlineAudio]
	if err := host.Call(ctx, "provider.online.audioDescribe", pluginproto.OnlineReference{Context: scope, ExternalID: "audio:1"}, &description); err != nil || client.opens.Load() != 0 {
		t.Fatal("audio description consumed audio", err)
	}
	var opened pluginproto.OnlineResult[pluginproto.OnlineAudioOpened]
	if err := host.Call(ctx, "provider.online.audioOpen", pluginproto.OnlineReference{Context: scope, ExternalID: "audio:1"}, &opened); err != nil {
		t.Fatal(err)
	}
	if client.opens.Load() != 1 || !pluginproto.ValidOnlineAudioID(opened.Value.StreamID) {
		t.Fatal("invalid opaque audio stream")
	}
	request := pluginproto.OnlineAudioRead{Context: scope, StreamID: opened.Value.StreamID}
	wrong := request
	wrong.Context.UserID = "another-owner"
	if err := host.Call(ctx, "provider.online.audioRead", wrong, nil); pluginproto.ErrorCode(err) != "ACCESS_DENIED" {
		t.Fatal("audio stream escaped owner scope", err)
	}
	produced := make(chan error, 1)
	go func() { _, err := writer.Write([]byte("first bytes")); produced <- err }()
	var chunk pluginproto.OnlineResult[pluginproto.OnlineAudioChunk]
	if err := host.Call(ctx, "provider.online.audioRead", request, &chunk); err != nil {
		t.Fatal("first chunk waited for the whole audio body", err)
	}
	if !bytes.Equal(chunk.Value.Data, []byte("first bytes")) || chunk.Value.EOF {
		t.Fatalf("unexpected chunk: %+v", chunk.Value)
	}
	if err := <-produced; err != nil {
		t.Fatal(err)
	}
	if err := host.Call(ctx, "provider.online.audioRead", request, nil); pluginproto.ErrorCode(err) != "RESOURCE_CHANGED" {
		t.Fatal("old byte offset was accepted", err)
	}
	if err := host.Call(ctx, "provider.online.audioClose", request, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte("closed")); err != io.ErrClosedPipe {
		t.Fatal("closing stream did not release source", err)
	}
	if err := host.Call(ctx, "provider.online.audioRead", request, nil); pluginproto.ErrorCode(err) != "NOT_FOUND" {
		t.Fatal("closed stream could be reopened", err)
	}
}
