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

	"github.com/fnMrRice/MusicFS-server/sdk/instanceid"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginlog"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginconfig"
)

type audioWorker struct {
	mu                sync.Mutex
	ready             bool
	capabilities      Capabilities
	active            bool
	instanceID, nonce string
	taskID            string
}

func RunWorker(ctx context.Context, input io.ReadCloser, output io.WriteCloser) error {
	w := &audioWorker{}
	peer := pluginproto.NewPeer(ctx, input, output, "w-", w.handle)
	peer.Wait()
	return peer.Err()
}

func (w *audioWorker) handle(ctx context.Context, method string, raw json.RawMessage) (any, error) {
	if method == "host.hello" {
		w.mu.Lock()
		defer w.mu.Unlock()
		var b bootstrap
		if json.Unmarshal(raw, &b) != nil || b.ProtocolVersion != pluginproto.ProtocolVersion || !instanceid.Valid(b.InstanceID) || b.TaskID == "" || b.Nonce == "" || w.ready {
			return nil, fmt.Errorf("PROTOCOL_MISMATCH: invalid audio handshake")
		}
		w.ready = true
		w.capabilities = b.Capabilities
		w.instanceID, w.nonce = b.InstanceID, b.Nonce
		w.taskID = b.TaskID
		capabilities := []string{"audio.decode", "audio.transcode", "audio.dsp", "audio.dsp.builtin", "audio.encode", "audio.mux"}
		if b.Capabilities.Normalization {
			capabilities = append(capabilities, "audio.normalize")
		}
		return pluginproto.Ready{InstanceID: b.InstanceID, Nonce: b.Nonce, Manifest: pluginproto.Manifest{PluginID: "musicfs.plugin.decoder.ffmpeg", Plugin: pluginconfig.Identity("musicfs.plugin.decoder.ffmpeg"), PluginVersion: audioPluginVersion, ProtocolVersion: pluginproto.ProtocolVersion, SupportsMultipleInstances: pluginconfig.SupportsMultipleInstances("musicfs.plugin.decoder.ffmpeg"), Roles: []string{pluginconfig.Role("musicfs.plugin.decoder.ffmpeg")}, Capabilities: capabilities, Systems: []string{runtime.GOOS}, Architectures: []string{runtime.GOARCH}, MaxMessageBytes: pluginproto.MaxMessageBytes, MaxPageItems: pluginproto.MaxPageItems}}, nil
	}
	w.mu.Lock()
	ready := w.ready
	capabilities := w.capabilities
	taskID := w.taskID
	ctx = pluginlog.Context(ctx, "musicfs.plugin.decoder.ffmpeg", w.instanceID)
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
	if method == "audio.decode" || method == "audio.transcode" {
		return w.codec(ctx, method[len("audio."):], raw)
	}
	if method != "audio.prepare" && method != "audio.start" {
		return nil, pluginproto.Failure("UNSUPPORTED", "unknown audio method")
	}
	var r StartRequest
	if json.Unmarshal(raw, &r) != nil {
		return nil, fmt.Errorf("ACCESS_DENIED: invalid audio request")
	}
	if _, err := bufferFrames(r.BufferFrames); err != nil {
		return nil, err
	}
	profile, supported := ProfileByID(r.Plan.Profile)
	if !supported || r.Plan.PositionMs < 0 || r.Plan.PositionMs > 24*60*60*1000 || r.Plan.TaskID != taskID || r.Plan.AssetID == "" {
		return nil, fmt.Errorf("UNSUPPORTED: invalid audio plan")
	}
	decodeOutput, err := DecodeOutput(r.Plan.DecodeOutput)
	if err != nil || !supportsDecodeOutput(capabilities, decodeOutput) {
		return nil, fmt.Errorf("UNSUPPORTED: decoder output format is unavailable")
	}
	if err := validateLoopback(r.InputURL); err != nil {
		return nil, err
	}
	if err := validateLoopback(r.OutputURL); err != nil {
		return nil, err
	}
	if !r.DisableEffects {
		if err := ValidateDSP(r.DSPChain, r.DSPPlugins); err != nil {
			return nil, err
		}
	}
	if method == "audio.prepare" {
		format, err := audio.FormatForLayout(profile.Layout)
		if err != nil {
			return nil, err
		}
		node, err := buildRequestedDSP(ctx, r, format)
		if err != nil {
			return nil, err
		}
		output := node.Output()
		node.Close()
		if output.Format != format {
			return nil, fmt.Errorf("UNSUPPORTED: DSP output differs from selected encoding layout")
		}
		return map[string]any{"profile": profile.ID, "positionOriginMs": r.Plan.PositionMs, "seekMode": profile.SeekMode}, nil
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
	request.Header.Set("Content-Type", profile.MIME)
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
