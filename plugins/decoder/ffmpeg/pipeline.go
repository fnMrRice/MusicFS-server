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

	"github.com/fnMrRice/MusicFS-server/sdk/audiotypes"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginlog"
	"github.com/fnMrRice/MusicFS-server/src/core/logging"

	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio/dspworker"
)

type Plan = audiotypes.Plan
type StartRequest struct {
	Plan              Plan               `json:"plan"`
	FFmpegPath        string             `json:"ffmpegPath"`
	FFmpegSHA256      string             `json:"ffmpegSHA256"`
	InputURL          string             `json:"inputUrl"`
	OutputURL         string             `json:"outputUrl"`
	DSPChain          []audio.NodeSpec   `json:"dspChain,omitempty"`
	DSPPlugins        []dspworker.Config `json:"dspPlugins,omitempty"`
	BufferFrames      int                `json:"bufferFrames,omitempty"`
	DisableEffects    bool               `json:"disableEffects,omitempty"`
	DisableChannelMix bool               `json:"disableChannelMix,omitempty"`
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
	frames, err := bufferFrames(r.BufferFrames)
	if err != nil {
		return err
	}
	profile, supported := ProfileByID(r.Plan.Profile)
	if !supported || r.Plan.PositionMs < 0 || r.Plan.PositionMs > 24*60*60*1000 {
		return fmt.Errorf("UNSUPPORTED: invalid audio plan")
	}
	decodeOutput, err := DecodeOutput(r.Plan.DecodeOutput)
	if err != nil {
		return err
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
	node, err := buildRequestedDSP(ctx, r, format)
	if err != nil {
		return err
	}
	defer node.Close()
	if node.Output().Format != format {
		return fmt.Errorf("UNSUPPORTED: DSP output differs from selected encoding layout")
	}
	requestCtx := ctx
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	targetFrame := r.Plan.PositionMs * int64(format.SampleRate) / 1000
	window := pcmWindow{DiscardFrames: targetFrame}
	guessChannels := "0"
	if r.DisableChannelMix {
		// 普通 PCM WAV 不带掩码，单/双声道按格式约定识别；不猜测多声道位置。
		guessChannels = "2"
	}
	decodeArgs := []string{"-hide_banner", "-nostdin", "-v", "error", "-protocol_whitelist", "http,tcp", "-format_whitelist", inputFormats, "-guess_layout_max", guessChannels, "-i", r.InputURL}
	if node.Stateless() {
		decodeArgs = append(decodeArgs, "-ss", strconv.FormatFloat(float64(r.Plan.PositionMs)/1000, 'f', 3, 64))
		window = pcmWindow{StartFrame: targetFrame}
	}
	// 有状态链从曲首重建历史；仅在 DSP 处理之后丢弃目标位置之前的帧。
	// WAV 扩展头携带原声道掩码，避免先下混后再上混丢失声道内容。
	decodeArgs = append(decodeArgs, "-map", "0:a:0", "-vn", "-ar", "48000")
	if r.DisableChannelMix {
		// 输出档案已优先保留来源声道；仅在编码器或客户端限制下由解码器兼容降混。
		decodeArgs = append(decodeArgs, "-ac", strconv.Itoa(profile.Channels), "-channel_layout", profile.Layout)
	}
	decodeArgs = append(decodeArgs, "-f", "wav", "-acodec", decodeOutput, "pipe:1")
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
	var converted io.Reader
	if r.DisableChannelMix {
		converted, err = decodedWAVWithoutMix(decoded, format, decodeOutput)
	} else {
		converted, err = convertedWAVReaderOutput(decoded, format, decodeOutput)
	}
	if err == nil {
		err = transferPCMFormatsWindow(ctx, converted, encodeInput, node, format, node.Output().Format, frames, window)
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
