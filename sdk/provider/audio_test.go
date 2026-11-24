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
	testClient
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

func startAudioWorker(t *testing.T, ctx context.Context, client *audioFixtureClient) (*pluginproto.Peer, pluginproto.Context) {
	t.Helper()
	descriptor := pluginproto.OnlinePolicy(online.Bilibili)
	descriptor.Plugin = &pluginproto.PluginIdentity{ID: pluginproto.OnlinePluginID(online.Bilibili), Name: "Audio fixture", LocalizedNames: map[string]string{}, Features: []string{"music-source"}}
	descriptor.Capabilities = []online.Capability{
		{Name: "stream.resolve", Supported: true, Reason: "Fixture audio"},
		{Name: "metadata.track", Supported: true, Reason: "Fixture metadata"},
	}
	plugin := provider.Plugin{Descriptor: descriptor, Version: "0.1.0", NewClient: func(online.Scope, time.Duration) (provider.Client, error) { return client, nil }}
	left, right := net.Pipe()
	done := make(chan error, 1)
	go func() { done <- provider.Run(ctx, plugin, right, right) }()
	host := pluginproto.NewPeer(ctx, left, left, "h-", nil)
	t.Cleanup(func() { host.Close(); <-done })
	hello := pluginproto.OnlineHello{Hello: pluginproto.Hello{ProtocolVersion: pluginproto.ProtocolVersion, InstanceID: instanceid.Canonical("network-audio"), Nonce: fault.NewID(), ConfigRevision: pluginproto.OnlineRevision}, UserID: "owner", TimeoutMs: 3000}
	var ready pluginproto.Ready
	if err := host.Call(ctx, "host.hello", hello, &ready); err != nil {
		t.Fatal(err)
	}
	return host, pluginproto.Context{UserID: hello.UserID, InstanceID: hello.InstanceID, TaskID: fault.NewID(), TimeoutMs: hello.TimeoutMs}
}

func TestAudioControlStreamIsLazyScopedAndSequential(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	reader, writer := io.Pipe()
	defer writer.Close()
	client := &audioFixtureClient{reader: reader}
	host, scope := startAudioWorker(t, ctx, client)
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

func TestActiveAudioStreamReadsWhileMetadataSlotsAreOccupied(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	payload := bytes.Repeat([]byte("a"), pluginproto.OnlineAudioChunkBytes+1)
	client := &audioFixtureClient{
		testClient: testClient{metadataStarted: make(chan struct{}, 2), metadataRelease: make(chan struct{})},
		reader:     io.NopCloser(bytes.NewReader(payload)),
	}
	host, scope := startAudioWorker(t, ctx, client)
	var opened pluginproto.OnlineResult[pluginproto.OnlineAudioOpened]
	if err := host.Call(ctx, "provider.online.audioOpen", pluginproto.OnlineAudioReference{Context: scope, ExternalID: "audio:1"}, &opened); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 2)
	for _, id := range []string{"51", "52"} {
		go func() {
			done <- host.Call(ctx, "provider.online.metadata", pluginproto.OnlineReference{Context: scope, ExternalID: id}, nil)
		}()
	}
	defer func() {
		close(client.metadataRelease)
		for range 2 {
			if err := <-done; err != nil {
				t.Error("admitted metadata request failed", err)
			}
		}
	}()
	for range 2 {
		select {
		case <-client.metadataStarted:
		case <-ctx.Done():
			t.Fatal("metadata did not occupy both slots")
		}
	}
	if err := host.Call(ctx, "provider.online.metadata", pluginproto.OnlineReference{Context: scope, ExternalID: "53"}, nil); pluginproto.ErrorCode(err) != "RATE_LIMITED" {
		t.Fatal("excess metadata request was not rate limited", err)
	}
	request := pluginproto.OnlineAudioRead{Context: scope, StreamID: opened.Value.StreamID}
	var received []byte
	for len(received) < len(payload) {
		var chunk pluginproto.OnlineResult[pluginproto.OnlineAudioChunk]
		if err := host.Call(ctx, "provider.online.audioRead", request, &chunk); err != nil {
			t.Fatal("active audio stream was interrupted", err)
		}
		if chunk.Value.Offset != request.Offset || len(chunk.Value.Data) == 0 {
			t.Fatalf("unexpected audio chunk at offset %d", request.Offset)
		}
		received = append(received, chunk.Value.Data...)
		request.Offset = int64(len(received))
	}
	if !bytes.Equal(received, payload) {
		t.Fatal("audio payload changed")
	}
	if err := host.Call(ctx, "provider.online.audioClose", request, nil); err != nil {
		t.Fatal("active audio stream did not close", err)
	}
}
