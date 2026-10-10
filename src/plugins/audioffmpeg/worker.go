package audioffmpeg

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"sync"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/audio/dsp"
	"github.com/fnMrRice/MusicFS-server/src/pluginproto"
)

type audioWorker struct {
	mu                sync.Mutex
	ready             bool
	capabilities      Capabilities
	active            bool
	instanceID, nonce string
}

func RunWorker(ctx context.Context, input io.ReadCloser, output io.WriteCloser) error {
	w := &audioWorker{}
	peer := pluginproto.NewPeer(ctx, input, output, "w-", w.handle)
	<-peer.Done()
	return peer.Err()
}

func (w *audioWorker) handle(ctx context.Context, method string, raw json.RawMessage) (any, error) {
	if method == "host.hello" {
		w.mu.Lock()
		defer w.mu.Unlock()
		var b bootstrap
		if json.Unmarshal(raw, &b) != nil || b.ProtocolVersion != pluginproto.ProtocolVersion || b.InstanceID == "" || b.Nonce == "" || w.ready {
			return nil, fmt.Errorf("PROTOCOL_MISMATCH: invalid audio handshake")
		}
		w.ready = true
		w.capabilities = b.Capabilities
		w.instanceID, w.nonce = b.InstanceID, b.Nonce
		capabilities := []string{"audio.decode", "audio.dsp", "audio.encode", "audio.mux"}
		if b.Capabilities.Normalization {
			capabilities = append(capabilities, "audio.normalize")
		}
		return pluginproto.Ready{InstanceID: b.InstanceID, Nonce: b.Nonce, Manifest: pluginproto.Manifest{PluginID: "musicfs.audioffmpeg", PluginVersion: "0.1.0", ProtocolVersion: pluginproto.ProtocolVersion, Roles: []string{"audio"}, Capabilities: capabilities, Systems: []string{runtime.GOOS}, Architectures: []string{runtime.GOARCH}, MaxMessageBytes: pluginproto.MaxMessageBytes, MaxPageItems: pluginproto.MaxPageItems}}, nil
	}
	w.mu.Lock()
	ready := w.ready
	capabilities := w.capabilities
	w.mu.Unlock()
	if !ready {
		return nil, fmt.Errorf("PROTOCOL_MISMATCH: handshake required")
	}
	if method == "audio.describe" {
		return capabilities, nil
	}
	if method == "audio.normalize" {
		return w.normalize(ctx, raw)
	}
	if method != "audio.prepare" && method != "audio.start" {
		return nil, pluginproto.Failure("UNSUPPORTED", "unknown audio method")
	}
	var r StartRequest
	if json.Unmarshal(raw, &r) != nil {
		return nil, fmt.Errorf("ACCESS_DENIED: invalid audio request")
	}
	if r.Plan.Profile != "mp3-128" || r.Plan.PositionMs < 0 || r.Plan.TaskID == "" || r.Plan.AssetID == "" {
		return nil, fmt.Errorf("UNSUPPORTED: invalid audio plan")
	}
	if err := validateLoopback(r.InputURL); err != nil {
		return nil, err
	}
	if err := validateLoopback(r.OutputURL); err != nil {
		return nil, err
	}
	node, err := dsp.NewGain(r.Plan.GainDB)
	if err != nil {
		return nil, err
	}
	node.Close()
	if method == "audio.prepare" {
		return map[string]any{"profile": "mp3-128", "positionOriginMs": r.Plan.PositionMs, "seekMode": "timeRestart"}, nil
	}
	w.mu.Lock()
	if w.active {
		w.mu.Unlock()
		return nil, fmt.Errorf("RATE_LIMITED: audio worker is busy")
	}
	w.active = true
	w.mu.Unlock()
	defer func() { w.mu.Lock(); w.active = false; w.mu.Unlock() }()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	reader, writer := io.Pipe()
	defer reader.Close()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, r.OutputURL, reader)
	if err != nil {
		return nil, fmt.Errorf("ACCESS_DENIED: invalid output channel")
	}
	request.Header.Set("Content-Type", "audio/mpeg")
	client := &http.Client{Transport: &http.Transport{Proxy: nil, ResponseHeaderTimeout: 30 * time.Minute}, CheckRedirect: func(*http.Request, []*http.Request) error {
		return fmt.Errorf("ACCESS_DENIED: channel redirect refused")
	}}
	defer client.CloseIdleConnections()
	posted := make(chan error, 1)
	go func() {
		response, err := client.Do(request)
		if err == nil {
			response.Body.Close()
			if response.StatusCode != http.StatusNoContent {
				err = fmt.Errorf("TEMPORARY_FAILURE: output channel failed")
			}
		}
		if err != nil {
			reader.CloseWithError(fmt.Errorf("output channel closed"))
			cancel()
		}
		posted <- err
	}()
	err = Pipeline(ctx, r, writer)
	writer.CloseWithError(err)
	postErr := <-posted
	if err != nil {
		return nil, err
	}
	if postErr != nil {
		return nil, fmt.Errorf("TEMPORARY_FAILURE: output delivery failed")
	}
	return map[string]string{"state": "complete"}, nil
}
