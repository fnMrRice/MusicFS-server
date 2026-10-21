package ncmdecoder

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"runtime"
	"sync"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/bundledplugin"
	"github.com/fnMrRice/MusicFS-server/sdk/instanceid"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginconfig"
)

type ncmWorker struct {
	mu     sync.Mutex
	ctx    context.Context
	hello  pluginproto.Hello
	opened bool
	input  *channelReader
	output *dataChannel
}

func RunWorker(ctx context.Context, input io.ReadCloser, output io.WriteCloser) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	w := &ncmWorker{ctx: ctx}
	peer := pluginproto.NewPeer(ctx, input, output, "w-", w.handle)
	<-peer.Done()
	cancel()
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.output != nil {
		w.output.Close()
	}
	if w.input != nil {
		w.input.Close()
	}
	return peer.Err()
}

func (w *ncmWorker) handle(ctx context.Context, method string, raw json.RawMessage) (any, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if method == "host.hello" {
		var hello pluginproto.Hello
		if decodeRequest(raw, &hello) != nil || hello.ProtocolVersion != pluginproto.ProtocolVersion || !instanceid.Valid(hello.InstanceID) || !validID(hello.TaskID) || !validNonce(hello.Nonce) || hello.ConfigRevision != "1" || w.hello.Nonce != "" {
			return nil, fmt.Errorf("PROTOCOL_MISMATCH: invalid NCM handshake")
		}
		w.hello = hello
		return pluginproto.Ready{InstanceID: hello.InstanceID, Nonce: hello.Nonce, Manifest: pluginproto.Manifest{
			PluginID: "musicfs.plugin.decoder.ncm", Plugin: pluginconfig.Identity("musicfs.plugin.decoder.ncm"), PluginVersion: bundledplugin.NCMVersion, ProtocolVersion: pluginproto.ProtocolVersion,
			SupportsMultipleInstances: pluginconfig.SupportsMultipleInstances("musicfs.plugin.decoder.ncm"), Roles: []string{pluginconfig.Role("musicfs.plugin.decoder.ncm")}, Capabilities: []string{"audio.decrypt"}, Systems: []string{runtime.GOOS}, Architectures: []string{runtime.GOARCH},
			MaxMessageBytes: pluginproto.MaxMessageBytes, MaxPageItems: pluginproto.MaxPageItems,
		}}, nil
	}
	if w.hello.Nonce == "" {
		return nil, fmt.Errorf("PROTOCOL_MISMATCH: NCM handshake required")
	}
	if method != "ncm.open" {
		return nil, pluginproto.Failure("UNSUPPORTED", "unknown NCM method")
	}
	if w.opened {
		return nil, pluginproto.Failure("CONFLICT", "NCM input is already bound")
	}
	w.opened = true
	var request openRequest
	if decodeRequest(raw, &request) != nil || request.TaskID != w.hello.TaskID || !validID(request.AssetID) || request.Nonce != w.hello.Nonce || request.Size <= 0 {
		return nil, fmt.Errorf("ACCESS_DENIED: invalid NCM task binding")
	}
	inputCtx, inputCancel := context.WithCancel(w.ctx)
	stop := context.AfterFunc(ctx, inputCancel)
	defer stop()
	reader, err := newChannelReader(inputCtx, request.InputURL, request.Nonce, "input", request.Size)
	if err != nil {
		inputCancel()
		return nil, err
	}
	w.input = reader
	decoded, err := NewReader(reader, request.Size)
	if err != nil {
		inputCancel()
		return nil, err
	}
	if ctx.Err() != nil || w.ctx.Err() != nil || !stop() {
		inputCancel()
		return nil, fmt.Errorf("CANCELED: NCM opening canceled")
	}
	channel, err := newDataChannel(w.ctx, request.Nonce, "output", decoded, decoded.Size())
	if err != nil {
		inputCancel()
		return nil, err
	}
	w.output = channel
	return openResult{TaskID: request.TaskID, AssetID: request.AssetID, Nonce: request.Nonce, OutputURL: channel.url, Size: decoded.Size(), Metadata: decoded.Metadata()}, nil
}
