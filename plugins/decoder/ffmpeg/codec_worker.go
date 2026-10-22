package audioffmpeg

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"slices"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/logging"
)

// CodecRequest 使用宿主创建的短期通道，禁止插件从请求取得任意文件路径。
type CodecRequest struct {
	TaskID         string `json:"taskId"`
	AssetID        string `json:"assetId"`
	OutputFormat   string `json:"outputFormat"`
	MaxOutputBytes int64  `json:"maxOutputBytes"`
	FFmpegPath     string `json:"ffmpegPath"`
	FFmpegSHA256   string `json:"ffmpegSHA256"`
	InputURL       string `json:"inputUrl"`
	OutputURL      string `json:"outputUrl"`
}

func (w *audioWorker) codec(ctx context.Context, mode string, raw json.RawMessage) (any, error) {
	var request CodecRequest
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&request) != nil || request.TaskID != w.taskID || request.AssetID == "" {
		return nil, fmt.Errorf("ACCESS_DENIED: invalid codec request")
	}
	limit, err := codecOutputLimit(request.MaxOutputBytes)
	if err != nil {
		return nil, err
	}
	if err = validateLoopback(request.InputURL); err != nil {
		return nil, err
	}
	if err = validateLoopback(request.OutputURL); err != nil {
		return nil, err
	}
	args, mime, err := codecArguments(mode, request.OutputFormat)
	if err != nil {
		return nil, err
	}
	if mode == "decode" && !supportsDecodeOutput(w.capabilities, request.OutputFormat) || mode == "transcode" && !slices.Contains(w.capabilities.TranscodeFormats, request.OutputFormat) {
		return nil, fmt.Errorf("UNSUPPORTED: codec output is not declared")
	}
	hash, err := executableHash(request.FFmpegPath)
	if err != nil || hash != request.FFmpegSHA256 || hash != w.capabilities.FFmpegSHA256 {
		return nil, fmt.Errorf("ACCESS_DENIED: codec executable changed")
	}
	w.mu.Lock()
	if w.active {
		w.mu.Unlock()
		return nil, fmt.Errorf("RATE_LIMITED: audio worker is busy")
	}
	w.active = true
	w.mu.Unlock()
	defer func() { w.mu.Lock(); w.active = false; w.mu.Unlock() }()
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	reader, writer := io.Pipe()
	defer reader.Close()
	post, err := http.NewRequestWithContext(ctx, http.MethodPost, request.OutputURL, reader)
	if err != nil {
		return nil, fmt.Errorf("ACCESS_DENIED: invalid codec output channel")
	}
	post.Header.Set("Content-Type", mime)
	client := &http.Client{Transport: &http.Transport{Proxy: nil, ResponseHeaderTimeout: 2 * time.Minute}, CheckRedirect: func(*http.Request, []*http.Request) error {
		return fmt.Errorf("ACCESS_DENIED: channel redirect refused")
	}}
	defer client.CloseIdleConnections()
	posted := make(chan error, 1)
	go func() {
		response, postErr := client.Do(post)
		if postErr == nil {
			response.Body.Close()
			if response.StatusCode != http.StatusNoContent {
				postErr = fmt.Errorf("TEMPORARY_FAILURE: codec output delivery failed")
			}
		}
		if postErr != nil {
			reader.CloseWithError(postErr)
			cancel()
		}
		posted <- postErr
	}()
	base := []string{"-hide_banner", "-nostdin", "-v", "error", "-protocol_whitelist", "http,tcp", "-format_whitelist", inputFormats}
	if mode == "decode" {
		// 解码交给后续 DSP 决定布局；显式转码使用 FFmpeg 自身的布局协商。
		base = append(base, "-guess_layout_max", "0")
	}
	base = append(base, "-i", request.InputURL, "-map", "0:a:0", "-vn", "-sn", "-dn", "-map_metadata", "-1")
	cmd := exec.CommandContext(ctx, request.FFmpegPath, append(base, args...)...)
	stderr := logging.PluginStderr("musicfs.plugin.decoder.ffmpeg", w.instanceID)
	defer stderr.Close()
	bounded := &codecOutputWriter{output: writer, limit: limit}
	cmd.Stderr, cmd.Stdout, cmd.WaitDelay = stderr, bounded, time.Second
	err = cmd.Run()
	writer.CloseWithError(err)
	postErr := <-posted
	if err != nil || postErr != nil {
		if bounded.err != nil {
			return nil, bounded.err
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("UNSUPPORTED: codec conversion failed")
	}
	return map[string]string{"state": "complete"}, nil
}

type transcodeOutput struct {
	container, mime string
	args            []string
}

func transcodeSpec(format string) (transcodeOutput, bool) {
	switch format {
	case "mp3":
		return transcodeOutput{"mp3", "audio/mpeg", []string{"-ac", "2", "-ar", "48000", "-c:a", "libmp3lame", "-b:a", "128k", "-f", "mp3", "-write_xing", "0", "-id3v2_version", "0", "pipe:1"}}, true
	case "flac":
		return transcodeOutput{"flac", "audio/flac", []string{"-c:a", "flac", "-f", "flac", "pipe:1"}}, true
	case "opus":
		return transcodeOutput{"webm", "audio/webm", []string{"-ac", "2", "-ar", "48000", "-c:a", "libopus", "-b:a", "128k", "-f", "webm", "pipe:1"}}, true
	case "wav":
		return transcodeOutput{"wav", "audio/wav", []string{"-c:a", "pcm_s16le", "-f", "wav", "pipe:1"}}, true
	}
	return transcodeOutput{}, false
}

func codecArguments(mode, output string) ([]string, string, error) {
	if mode == "decode" {
		format, err := DecodeOutput(output)
		return []string{"-ar", "48000", "-c:a", format, "-f", "wav", "pipe:1"}, "audio/wav", err
	}
	if mode == "transcode" {
		if spec, ok := transcodeSpec(output); ok {
			return spec.args, spec.mime, nil
		}
	}
	return nil, "", fmt.Errorf("UNSUPPORTED: unsupported codec conversion")
}
