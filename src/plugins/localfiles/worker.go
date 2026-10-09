package localfiles

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"runtime"
	"sync"

	"github.com/fnMrRice/MusicFS-server/src/pluginproto"
)

type worker struct {
	peer     *pluginproto.Peer
	mu       sync.Mutex
	ready    bool
	instance string
}

func Run(ctx context.Context, input io.ReadCloser, output io.WriteCloser) error {
	w := &worker{}
	w.peer = pluginproto.NewPeer(ctx, input, output, "w-", w.handle)
	<-w.peer.Done()
	return w.peer.Err()
}

func (w *worker) handle(ctx context.Context, method string, raw json.RawMessage) (any, error) {
	if method == "host.hello" {
		w.mu.Lock()
		defer w.mu.Unlock()
		var hello pluginproto.Hello
		if json.Unmarshal(raw, &hello) != nil || hello.ProtocolVersion != pluginproto.ProtocolVersion || hello.InstanceID == "" || hello.Nonce == "" || w.ready {
			return nil, fmt.Errorf("PROTOCOL_MISMATCH: invalid handshake")
		}
		w.ready = true
		w.instance = hello.InstanceID
		return pluginproto.Ready{InstanceID: hello.InstanceID, Nonce: hello.Nonce, Manifest: pluginproto.Manifest{PluginID: "musicfs.localfiles", PluginVersion: "0.1.0", ProtocolVersion: pluginproto.ProtocolVersion, Roles: []string{"provider"}, Capabilities: []string{"library.scan", "metadata.read", "stream.resolve"}, Systems: []string{runtime.GOOS}, Architectures: []string{runtime.GOARCH}, MaxMessageBytes: pluginproto.MaxMessageBytes, MaxPageItems: pluginproto.MaxPageItems}}, nil
	}
	w.mu.Lock()
	ready := w.ready
	w.mu.Unlock()
	if !ready {
		return nil, fmt.Errorf("PROTOCOL_MISMATCH: handshake required")
	}
	switch method {
	case "provider.scan":
		var r pluginproto.ScanRequest
		if json.Unmarshal(raw, &r) != nil {
			return nil, fmt.Errorf("ACCESS_DENIED: invalid request")
		}
		return w.scan(ctx, r)
	case "provider.resolve", "provider.metadata":
		var r pluginproto.ResolveRequest
		if json.Unmarshal(raw, &r) != nil {
			return nil, fmt.Errorf("ACCESS_DENIED: invalid request")
		}
		return w.resolve(ctx, r, method == "provider.metadata")
	default:
		return nil, pluginproto.Failure("UNSUPPORTED", "unknown provider method")
	}
}
