package audioffmpeg

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/audioformat"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
)

const DefaultDecodeOutputBytes int64 = 256 << 20
const MaxDecodeOutputBytes int64 = 1 << 30

// AudioDecoder 将插件自己的识别、优先级及解码行为装入通用解码链。
// 原生表示无需工具进程，PCM 解码则要求已验证的 FFmpeg 实例。
func (p *Processor) AudioDecoder() audioformat.Decoder {
	return audioformat.Decoder{Detector: FormatDetector(), Decode: p.Decode}
}

func (p *Processor) Decode(ctx context.Context, request audioformat.Request, input audioformat.Input) (audioformat.Output, error) {
	if err := ctx.Err(); err != nil {
		return audioformat.Output{}, err
	}
	if request.OutputFormat == "native" {
		registry, _ := audioformat.New(FormatDetector())
		match, found, err := registry.Detect(input.Reader, input.Size)
		if err != nil {
			return audioformat.Output{}, err
		}
		if !found {
			return audioformat.Output{}, audioformat.ErrUnsupported
		}
		input.Format, input.MIME = match.Format, match.MIME
		return audioformat.Output{Input: input, NeedsDecoding: false}, nil
	}
	format, err := DecodeOutput(request.OutputFormat)
	if err != nil {
		return audioformat.Output{}, audioformat.ErrUnsupported
	}
	request.OutputFormat = format
	if err = p.SupportsDecodeOutput(format); err != nil {
		return audioformat.Output{}, err
	}
	return p.transform(ctx, request, input, "decode", "wav", "audio/wav")
}

// Transcode 是显式编码转换能力，不加入自动解码循环，也不执行播放 DSP。
func (p *Processor) Transcode(ctx context.Context, request audioformat.Request, input audioformat.Input) (audioformat.Output, error) {
	spec, ok := transcodeSpec(request.OutputFormat)
	if !ok {
		return audioformat.Output{}, audioformat.ErrUnsupported
	}
	if p == nil || !slices.Contains(p.Capabilities.TranscodeFormats, request.OutputFormat) {
		return audioformat.Output{}, fmt.Errorf("UNSUPPORTED: requested transcode format is unavailable")
	}
	return p.transform(ctx, request, input, "transcode", spec.container, spec.mime)
}

func (p *Processor) transform(ctx context.Context, request audioformat.Request, input audioformat.Input, mode, format, mime string) (_ audioformat.Output, transformErr error) {
	if request.TaskID == "" || request.AssetID == "" || input.Reader == nil || input.Size <= 0 {
		return audioformat.Output{}, fmt.Errorf("ACCESS_DENIED: invalid codec task input")
	}
	limit, err := codecOutputLimit(request.MaxOutputBytes)
	if err != nil {
		return audioformat.Output{}, err
	}
	lease, err := p.AcquireLease(ctx)
	if err != nil {
		return audioformat.Output{}, err
	}
	transferred := false
	defer func() {
		if !transferred {
			lease.Close()
		}
	}()
	if lease != nil {
		ctx = lease.Context()
	}
	select {
	case p.slots <- struct{}{}:
		defer func() { <-p.slots }()
	case <-ctx.Done():
		return audioformat.Output{}, ctx.Err()
	}
	file, err := os.CreateTemp(p.config.WorkDir, "musicfs-codec-*.tmp")
	if err != nil {
		return audioformat.Output{}, fmt.Errorf("TEMPORARY_FAILURE: codec output storage unavailable")
	}
	cleanup := codecFileCleanup(file, lease.Close)
	success := false
	defer func() {
		if !success {
			transformErr = errors.Join(transformErr, cleanup())
		}
	}()
	workCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	output := &codecOutputWriter{output: file, limit: limit}
	err = p.run(workCtx, request.TaskID, input.Reader, input.Size, output, func(peer *pluginproto.Peer, inputURL, outputURL string) error {
		return peer.Call(workCtx, "audio."+mode, CodecRequest{
			TaskID: request.TaskID, AssetID: request.AssetID, OutputFormat: request.OutputFormat, MaxOutputBytes: limit,
			FFmpegPath: p.config.FFmpegPath, FFmpegSHA256: p.Capabilities.FFmpegSHA256, InputURL: inputURL, OutputURL: outputURL,
		}, nil)
	})
	if err != nil {
		return audioformat.Output{}, err
	}
	if err = ctx.Err(); err != nil {
		return audioformat.Output{}, err
	}
	if output.bytes == 0 {
		return audioformat.Output{}, fmt.Errorf("UNSUPPORTED: codec produced no audio data")
	}
	stopCleanup := context.AfterFunc(ctx, func() { _ = cleanup() })
	transferred = true
	success = true
	return audioformat.Output{Input: audioformat.Input{Reader: file, Size: output.bytes, Format: format, MIME: mime, Metadata: input.Metadata}, NeedsDecoding: false,
		Close: func() error { stopCleanup(); return cleanup() }}, nil
}

// 文件句柄只关闭一次，删除失败可重试；占用额度由调用方等到成功删除后释放。
func codecFileCleanup(file *os.File, release func()) func() error {
	var mu sync.Mutex
	closed := false
	var closeErr error
	return func() error {
		mu.Lock()
		defer mu.Unlock()
		if !closed {
			closeErr = file.Close()
			closed = true
			release()
		}
		removeErr := os.Remove(file.Name())
		if os.IsNotExist(removeErr) {
			removeErr = nil
		}
		if closeErr != nil || removeErr != nil {
			return errors.Join(audioformat.ErrCleanup, closeErr, removeErr)
		}
		return nil
	}
}

func codecOutputLimit(value int64) (int64, error) {
	if value == 0 {
		value = DefaultDecodeOutputBytes
	}
	if value < 1 || value > MaxDecodeOutputBytes {
		return 0, fmt.Errorf("INVALID_ARGUMENT: invalid codec output limit")
	}
	return value, nil
}

type codecOutputWriter struct {
	output       io.Writer
	bytes, limit int64
	err          error
}

func (w *codecOutputWriter) Write(data []byte) (int, error) {
	if int64(len(data)) > w.limit-w.bytes {
		w.err = fmt.Errorf("RATE_LIMITED: codec output exceeds task budget")
		return 0, w.err
	}
	n, err := w.output.Write(data)
	w.bytes += int64(n)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	w.err = err
	return n, err
}
