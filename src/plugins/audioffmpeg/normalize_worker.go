package audioffmpeg

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/pluginproto"
)

func (w *audioWorker) normalize(ctx context.Context, raw json.RawMessage) (any, error) {
	var r NormalizeRequest
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&r) != nil {
		return nil, fmt.Errorf("ACCESS_DENIED: invalid normalization request")
	}
	if _, _, err := r.Plan.Samples(); err != nil {
		return nil, err
	}
	w.mu.Lock()
	capabilities, instance, nonce := w.capabilities, w.instanceID, w.nonce
	w.mu.Unlock()
	if !capabilities.Normalization {
		return nil, fmt.Errorf("UNSUPPORTED: normalization components unavailable")
	}
	if instance != "audio-"+r.Plan.TaskID || r.FFmpegSHA256 != capabilities.FFmpegSHA256 {
		return nil, fmt.Errorf("ACCESS_DENIED: normalization task identity mismatch")
	}
	if err := validateLoopback(r.InputURL); err != nil {
		return nil, err
	}
	if err := validateLoopback(r.OutputURL); err != nil {
		return nil, err
	}
	input, _ := url.Parse(r.InputURL)
	output, _ := url.Parse(r.OutputURL)
	if input.Host != output.Host || input.Path != "/"+nonce+"/input" || output.Path != "/"+nonce+"/output" {
		return nil, fmt.Errorf("ACCESS_DENIED: normalization channel binding mismatch")
	}
	w.mu.Lock()
	if w.active {
		w.mu.Unlock()
		return nil, fmt.Errorf("RATE_LIMITED: audio worker is busy")
	}
	w.active = true
	w.mu.Unlock()
	defer func() { w.mu.Lock(); w.active = false; w.mu.Unlock() }()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	reader, writer := io.Pipe()
	defer reader.Close()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, r.OutputURL, reader)
	if err != nil {
		return nil, fmt.Errorf("ACCESS_DENIED: invalid normalization output")
	}
	request.Header.Set("Content-Type", "application/octet-stream")
	client := &http.Client{Transport: &http.Transport{Proxy: nil, ResponseHeaderTimeout: 30 * time.Second}, CheckRedirect: func(*http.Request, []*http.Request) error {
		return fmt.Errorf("ACCESS_DENIED: normalization redirect refused")
	}}
	defer client.CloseIdleConnections()
	posted := make(chan error, 1)
	go func() {
		response, err := client.Do(request)
		if err == nil {
			response.Body.Close()
			if response.StatusCode != http.StatusNoContent {
				err = fmt.Errorf("TEMPORARY_FAILURE: normalization output rejected")
			}
		}
		if err != nil {
			reader.CloseWithError(err)
			cancel()
		}
		posted <- err
	}()
	info, err := NormalizePipeline(ctx, r, writer)
	writer.CloseWithError(err)
	postErr := <-posted
	if err != nil {
		return nil, err
	}
	if postErr != nil {
		return nil, pluginproto.Failure("TEMPORARY_FAILURE", "Normalized PCM delivery failed")
	}
	info.DecoderVersion = capabilities.Version
	return info, nil
}
