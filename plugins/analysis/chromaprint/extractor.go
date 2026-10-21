package chromaprint

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/metadata/identification/fingerprint"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
)

type Extractor struct {
	config     Config
	normalizer Normalizer
	identity   Identity
	slots      chan struct{}
}

func New(ctx context.Context, c Config, normalizer Normalizer) (*Extractor, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c.Timeout == 0 {
		c.Timeout = 30 * time.Second
	}
	if c.MaxConcurrent == 0 {
		c.MaxConcurrent = 1
	}
	if normalizer == nil || c.MaxConcurrent < 1 || c.MaxConcurrent > 2 || c.Timeout < time.Second || c.Timeout > 2*time.Minute || !validSHA(c.FFmpegSHA256) || c.FFmpegVersion == "" || len(c.FFmpegVersion) > 512 {
		return nil, fmt.Errorf("INVALID_ARGUMENT: invalid fingerprint configuration")
	}
	if err := verifyTool(c.FpcalcPath, c.FpcalcSHA256); err != nil {
		return nil, err
	}
	version, err := probeTool(ctx, c.FpcalcPath)
	if err != nil {
		return nil, err
	}
	identity := Identity{Algorithm: "chromaprint", AlgorithmVersion: AlgorithmVersion, AlgorithmConfig: "cli-2-raw-v1", NormalizationRevision: audio.NormalizationRevision, QualityRevision: QualityRevision, Fpcalc: ToolIdentity{Version: version, SHA256: c.FpcalcSHA256}, Decoder: ToolIdentity{Version: c.FFmpegVersion, SHA256: c.FFmpegSHA256}}
	identity.ConfigRevision = identity.Revision()
	return &Extractor{config: c, normalizer: normalizer, identity: identity, slots: make(chan struct{}, c.MaxConcurrent)}, nil
}

func (e *Extractor) Identity() Identity { return e.identity }
func (e *Extractor) Revision() string   { return e.identity.ConfigRevision }

type countedPCM struct {
	ctx          context.Context
	out          io.Writer
	count, limit int64
}

func (w *countedPCM) Write(p []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	if int64(len(p)) > w.limit-w.count {
		return 0, fmt.Errorf("PROTOCOL_MISMATCH: normalization output exceeds range")
	}
	n, err := w.out.Write(p)
	w.count += int64(n)
	if err == nil && n != len(p) {
		err = io.ErrShortWrite
	}
	return n, err
}

func (e *Extractor) Extract(parent context.Context, r Request, input io.ReaderAt, size int64) (Detail, error) {
	var detail Detail
	if err := parent.Err(); err != nil {
		return detail, err
	}
	start, count, err := r.Samples()
	if err != nil {
		return detail, err
	}
	if input == nil || size <= 0 {
		return detail, fmt.Errorf("INVALID_ARGUMENT: invalid fingerprint input")
	}
	select {
	case e.slots <- struct{}{}:
		defer func() { <-e.slots }()
	default:
		return detail, fmt.Errorf("RATE_LIMITED: fingerprint processing is busy")
	}
	ctx, cancel := context.WithTimeout(parent, e.config.Timeout)
	defer cancel()
	if err = verifyTool(e.config.FpcalcPath, e.config.FpcalcSHA256); err != nil {
		return detail, err
	}
	cmd := command(ctx, e.config.FpcalcPath, "-format", "s16le", "-rate", "11025", "-channels", "1", "-algorithm", "2", "-length", "0", "-raw", "-json", "-")
	stdout, stderr := &boundedBuffer{limit: MaxDetailBytes}, &boundedBuffer{limit: 16 << 10}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return detail, err
	}
	if err = cmd.Start(); err != nil {
		return detail, fmt.Errorf("UNSUPPORTED: fingerprint executable failed to start")
	}
	cleanup, err := containProcess(cmd.Process.Pid)
	if err != nil {
		cmd.Process.Kill()
		cmd.Wait()
		return detail, fmt.Errorf("UNSUPPORTED: fingerprint process containment unavailable")
	}
	defer cleanup()
	pcm := &countedPCM{ctx: ctx, out: stdin, limit: count * 2}
	info, normalizeErr := e.normalizer.Normalize(ctx, r, input, size, pcm)
	stdin.Close()
	if normalizeErr != nil {
		cancel()
	}
	toolErr := cmd.Wait()
	if parent.Err() != nil {
		return detail, parent.Err()
	}
	if normalizeErr != nil {
		return detail, normalizeErr
	}
	if ctx.Err() != nil {
		return detail, ctx.Err()
	}
	if stdout.exceeded || stderr.exceeded {
		return detail, fmt.Errorf("PROTOCOL_MISMATCH: fingerprint tool output exceeds limit")
	}
	if info.Format != audio.FingerprintPCM || info.NormalizationRevision != audio.NormalizationRevision || info.StartSample != start || info.SampleCount <= 0 || info.SampleCount > count || info.SampleCount*2 != pcm.count || info.DecoderVersion != e.config.FFmpegVersion || info.DecoderSHA256 != e.config.FFmpegSHA256 {
		return detail, fmt.Errorf("PROTOCOL_MISMATCH: normalization evidence mismatch")
	}
	values := []uint32{}
	if toolErr == nil {
		var result struct {
			Duration    float64  `json:"duration"`
			Fingerprint []uint32 `json:"fingerprint"`
		}
		decoder := json.NewDecoder(bytes.NewReader(stdout.data))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&result) != nil || decoder.Decode(new(any)) != io.EOF || len(result.Fingerprint) > 1000 || result.Fingerprint == nil {
			return detail, fmt.Errorf("PROTOCOL_MISMATCH: invalid fingerprint result")
		}
		values = result.Fingerprint
	} else if info.SampleCount >= 110250 {
		return detail, fmt.Errorf("TEMPORARY_FAILURE: fingerprint extraction failed")
	}
	detail = Detail{SchemaVersion: 1, Identity: e.identity, RequestedStartMs: r.StartMs, RequestedDurationMs: r.DurationMs, PCM: info, Quality: fingerprint.Assess(values, info.SampleCount), Values: values}
	if _, err = detail.Encode(); err != nil {
		return Detail{}, err
	}
	return detail, nil
}
