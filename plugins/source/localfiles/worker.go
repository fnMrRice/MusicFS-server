package localfiles

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"runtime"
	"sync"

	"github.com/fnMrRice/MusicFS-server/plugins/source/localfiles/descriptor"
	"github.com/fnMrRice/MusicFS-server/sdk/bundledplugin"
	"github.com/fnMrRice/MusicFS-server/sdk/directoryidentity"
	"github.com/fnMrRice/MusicFS-server/sdk/instanceid"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginlog"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginconfig"
)

type worker struct {
	peer       *pluginproto.Peer
	mu         sync.Mutex
	identityMu sync.Mutex
	ready      bool
	instance   string
}

func Run(ctx context.Context, input io.ReadCloser, output io.WriteCloser) error {
	w := &worker{}
	w.peer = pluginproto.NewPeer(ctx, input, output, "w-", w.handle)
	w.peer.Wait()
	return w.peer.Err()
}

func (w *worker) handle(ctx context.Context, method string, raw json.RawMessage) (any, error) {
	if method == "host.hello" {
		w.mu.Lock()
		defer w.mu.Unlock()
		var hello pluginproto.Hello
		if json.Unmarshal(raw, &hello) != nil || hello.ProtocolVersion != pluginproto.ProtocolVersion || !instanceid.Valid(hello.InstanceID) || hello.TaskID != "" || hello.Nonce == "" || w.ready {
			return nil, fmt.Errorf("PROTOCOL_MISMATCH: invalid handshake")
		}
		w.ready = true
		w.instance = hello.InstanceID
		identity := Identity()
		return pluginproto.Ready{InstanceID: hello.InstanceID, Nonce: hello.Nonce, Manifest: pluginproto.Manifest{PluginID: identity.ID, Plugin: identity, PluginVersion: bundledplugin.LocalFilesVersion, ProtocolVersion: pluginproto.ProtocolVersion, SupportsMultipleInstances: pluginconfig.SupportsMultipleInstances(identity.ID), SourceType: pluginproto.SourceLocal, Roles: []string{pluginconfig.Role(identity.ID)}, Capabilities: []string{"library.scan", "metadata.read", "stream.resolve", "track.details", "library.packages", directoryidentity.Capability}, Configuration: descriptor.Configuration(), Systems: []string{runtime.GOOS}, Architectures: []string{runtime.GOARCH}, MaxMessageBytes: pluginproto.MaxMessageBytes, MaxPageItems: pluginproto.MaxPageItems}}, nil
	}
	w.mu.Lock()
	ready := w.ready
	ctx = pluginlog.Context(ctx, "musicfs.plugin.source.localfiles", w.instance)
	w.mu.Unlock()
	if !ready {
		return nil, fmt.Errorf("PROTOCOL_MISMATCH: handshake required")
	}
	switch method {
	case "provider.observePackage":
		var request pluginproto.PackageObserveRequest
		if err := pluginproto.DecodeOnline(raw, &request); err != nil {
			return nil, err
		}
		return w.observePackage(ctx, request)
	case "plugin.displayCount":
		return w.displayCount(raw)
	case "provider.scan":
		var r pluginproto.ScanRequest
		if json.Unmarshal(raw, &r) != nil {
			return nil, fmt.Errorf("ACCESS_DENIED: invalid request")
		}
		return w.scan(ctx, r)
	case "provider.trackDetails":
		var r pluginproto.ResolveRequest
		if err := pluginproto.DecodeOnline(raw, &r); err != nil {
			return nil, err
		}
		return w.trackDetails(ctx, r)
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
