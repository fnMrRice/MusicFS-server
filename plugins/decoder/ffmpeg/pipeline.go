package audioffmpeg

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os/exec"
	"strconv"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginlog"
	"github.com/fnMrRice/MusicFS-server/src/core/logging"

	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio/dspworker"
)

type Plan struct {
	TaskID     string  `json:"taskId"`
	AssetID    string  `json:"assetId"`
	Profile    string  `json:"profile"`
	GainDB     float64 `json:"gainDB"`
	PositionMs int64   `json:"positionMs"`
}
type StartRequest struct {
	Plan         Plan               `json:"plan"`
	FFmpegPath   string             `json:"ffmpegPath"`
	FFmpegSHA256 string             `json:"ffmpegSHA256"`
	InputURL     string             `json:"inputUrl"`
	OutputURL    string             `json:"outputUrl"`
	DSPChain     []audio.NodeSpec   `json:"dspChain,omitempty"`
	DSPPlugins   []dspworker.Config `json:"dspPlugins,omitempty"`
}

const inputFormats = "wav,mp3,flac,aiff,ogg,mov,matroska,webm,aac"

func validateLoopback(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("ACCESS_DENIED: invalid task channel")
	}
	return nil
}

func Pipeline(ctx context.Context, r StartRequest, output io.Writer) (pipelineErr error) {
	finish := pluginlog.Operation(ctx, "audio.pipeline")
	defer func() { finish(pipelineErr) }()
	if ctx.Err() != nil {
		return fmt.Errorf("CANCELED: audio processing stopped")
	}
	profile, supported := ProfileByID(r.Plan.Profile)
	if !supported || r.Plan.PositionMs < 0 || r.Plan.PositionMs > 24*60*60*1000 {
		return fmt.Errorf("UNSUPPORTED: invalid audio plan")
	}
	format, err := audio.FormatForLayout(profile.Layout)
	if err != nil {
		return err
	}
	if err := validateLoopback(r.InputURL); err != nil {
		return err
	}
	hash, err := executableHash(r.FFmpegPath)
	if err != nil || hash != r.FFmpegSHA256 {
		return fmt.Errorf("ACCESS_DENIED: audio executable changed")
	}
	node, err := buildDSPFormat(ctx, r.Plan.GainDB, r.DSPChain, r.DSPPlugins, format)
	if err != nil {
		return err
	}
	defer node.Close()
	requestCtx := ctx
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	targetFrame := r.Plan.PositionMs * int64(format.SampleRate) / 1000
	window := pcmWindow{DiscardFrames: targetFrame}
	decodeArgs := []string{"-hide_banner", "-nostdin", "-v", "error", "-protocol_whitelist", "http,tcp", "-format_whitelist", inputFormats, "-guess_layout_max", "0", "-i", r.InputURL}
	if node.Stateless() {
		decodeArgs = append(decodeArgs, "-ss", strconv.FormatFloat(float64(r.Plan.PositionMs)/1000, 'f', 3, 64))
		window = pcmWindow{StartFrame: targetFrame}
	}
	// 有状态链从曲首重建历史；仅在 DSP 处理之后丢弃目标位置之前的帧。
	// WAV 扩展头携带原声道掩码，避免先下混后再上混丢失声道内容。
	decodeArgs = append(decodeArgs, "-map", "0:a:0", "-vn", "-ar", "48000", "-f", "wav", "-acodec", "pcm_f32le", "pipe:1")
	decoder := exec.CommandContext(ctx, r.FFmpegPath, decodeArgs...)
	encoder := exec.CommandContext(ctx, r.FFmpegPath, encoderArguments(profile)...)
	decodeLog := logging.PluginStderr("musicfs.plugin.decoder.ffmpeg", "")
	encodeLog := logging.PluginStderr("musicfs.plugin.decoder.ffmpeg", "")
	defer decodeLog.Close()
	defer encodeLog.Close()
	decoder.Stderr, encoder.Stderr = decodeLog, encodeLog
	decoder.WaitDelay = time.Second
	encoder.WaitDelay = time.Second
	decoded, err := decoder.StdoutPipe()
	if err != nil {
		return err
	}
	encodeInput, err := encoder.StdinPipe()
	if err != nil {
		return err
	}
	encoder.Stdout = output
	if err = encoder.Start(); err != nil {
		if requestCtx.Err() != nil {
			return fmt.Errorf("CANCELED: audio processing stopped")
		}
		return fmt.Errorf("TEMPORARY_FAILURE: encoder failed to start")
	}
	if err = decoder.Start(); err != nil {
		cancel()
		encodeInput.Close()
		encoder.Wait()
		if requestCtx.Err() != nil {
			return fmt.Errorf("CANCELED: audio processing stopped")
		}
		return fmt.Errorf("TEMPORARY_FAILURE: decoder failed to start")
	}
	converted, err := convertedWAVReader(decoded, format)
	if err == nil {
		err = transferPCMFormatWindow(ctx, converted, encodeInput, node, format, window)
	}
	closeErr := encodeInput.Close()
	if err == nil && closeErr != nil {
		err = fmt.Errorf("TEMPORARY_FAILURE: encoder input close failed")
	}
	if err != nil {
		cancel()
	}
	decodeErr := decoder.Wait()
	if decodeErr != nil {
		cancel()
	}
	encodeErr := encoder.Wait()
	if requestCtx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("CANCELED: audio processing stopped")
	}
	if err != nil {
		return err
	}
	if decodeErr != nil || encodeErr != nil {
		return fmt.Errorf("TEMPORARY_FAILURE: audio processing failed")
	}
	return nil
}
