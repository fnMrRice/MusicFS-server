package audioffmpeg

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/audio"
	"github.com/fnMrRice/MusicFS-server/src/pluginproto"
)

type NormalizeRequest struct {
	Plan         audio.NormalizeRequest `json:"plan"`
	FFmpegPath   string                 `json:"ffmpegPath"`
	FFmpegSHA256 string                 `json:"ffmpegSHA256"`
	InputURL     string                 `json:"inputUrl"`
	OutputURL    string                 `json:"outputUrl"`
}

func (p *Processor) Normalize(ctx context.Context, plan audio.NormalizeRequest, input io.ReaderAt, size int64, output io.Writer) (audio.PCMInfo, error) {
	var info audio.PCMInfo
	if err := ctx.Err(); err != nil {
		return info, err
	}
	if input == nil || output == nil || size <= 0 {
		return info, fmt.Errorf("ACCESS_DENIED: invalid normalization input")
	}
	start, count, err := plan.Samples()
	if err != nil {
		return info, err
	}
	if !p.Capabilities.Normalization {
		return info, fmt.Errorf("UNSUPPORTED: PCM normalization components unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	// Background analysis cannot consume foreground playback admission slots.
	// One normalization worker runs at a time; waiting consumes the same deadline.
	select {
	case p.normalizeSlot <- struct{}{}:
		defer func() { <-p.normalizeSlot }()
	case <-ctx.Done():
		return info, ctx.Err()
	}
	w := &pcmWriter{output: output, limit: count * 2}
	err = p.run(ctx, plan.TaskID, input, size, w, func(peer *pluginproto.Peer, inputURL, outputURL string) error {
		request := NormalizeRequest{Plan: plan, FFmpegPath: p.config.FFmpegPath, FFmpegSHA256: p.Capabilities.FFmpegSHA256, InputURL: inputURL, OutputURL: outputURL}
		return peer.Call(ctx, "audio.normalize", request, &info)
	})
	if ctx.Err() != nil {
		return audio.PCMInfo{}, ctx.Err()
	}
	if err != nil {
		return audio.PCMInfo{}, err
	}
	if info.Format != audio.FingerprintPCM || info.NormalizationRevision != audio.NormalizationRevision || info.StartSample != start || info.SampleCount < 1 || info.SampleCount > count || w.bytes != info.SampleCount*2 || info.DecoderSHA256 != p.Capabilities.FFmpegSHA256 || info.DecoderVersion != p.Capabilities.Version {
		return audio.PCMInfo{}, fmt.Errorf("PROTOCOL_MISMATCH: invalid normalized PCM description")
	}
	return info, nil
}

type pcmWriter struct {
	output       io.Writer
	bytes, limit int64
}

func (w *pcmWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > w.limit-w.bytes {
		return 0, fmt.Errorf("PROTOCOL_MISMATCH: normalized PCM exceeds requested range")
	}
	n, err := w.output.Write(p)
	w.bytes += int64(n)
	if err == nil && n != len(p) {
		err = io.ErrShortWrite
	}
	return n, err
}

// NormalizePipeline deliberately has no DSP nodes or user preferences.
func NormalizePipeline(ctx context.Context, r NormalizeRequest, output io.Writer) (audio.PCMInfo, error) {
	var info audio.PCMInfo
	start, count, err := r.Plan.Samples()
	if err != nil {
		return info, err
	}
	if err = validateLoopback(r.InputURL); err != nil {
		return info, err
	}
	hash, err := executableHash(r.FFmpegPath)
	if err != nil || hash != r.FFmpegSHA256 {
		return info, fmt.Errorf("ACCESS_DENIED: normalization executable changed")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	args := []string{"-hide_banner", "-nostdin", "-v", "error", "-protocol_whitelist", "http,tcp", "-format_whitelist", inputFormats, "-i", r.InputURL, "-ss", strconv.FormatFloat(float64(start)/11025, 'f', 9, 64), "-t", strconv.FormatFloat(float64(count)/11025, 'f', 9, 64), "-map", "0:a:0", "-vn", "-sn", "-dn", "-ac", "1", "-ar", "11025", "-sample_fmt", "s16", "-c:a", "pcm_s16le", "-f", "s16le", "pipe:1"}
	cmd := exec.CommandContext(ctx, r.FFmpegPath, args...)
	cmd.Stderr = io.Discard
	cmd.WaitDelay = time.Second
	w := &pcmWriter{output: output, limit: count * 2}
	cmd.Stdout = w
	if err = cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return info, ctx.Err()
		}
		return info, fmt.Errorf("UNSUPPORTED: audio normalization failed")
	}
	if w.bytes == 0 || w.bytes%2 != 0 {
		return info, fmt.Errorf("UNSUPPORTED: no complete normalized PCM samples")
	}
	info = audio.PCMInfo{Format: audio.FingerprintPCM, NormalizationRevision: audio.NormalizationRevision, StartSample: start, SampleCount: w.bytes / 2, DecoderSHA256: hash}
	return info, nil
}
