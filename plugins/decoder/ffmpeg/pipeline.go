package audioffmpeg

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	"os/exec"
	"strconv"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/audio"
	"github.com/fnMrRice/MusicFS-server/src/audio/dspworker"
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

func Pipeline(ctx context.Context, r StartRequest, output io.Writer) error {
	if r.Plan.Profile != "mp3-128" || r.Plan.PositionMs < 0 || r.Plan.PositionMs > 24*60*60*1000 {
		return fmt.Errorf("UNSUPPORTED: invalid audio plan")
	}
	if err := validateLoopback(r.InputURL); err != nil {
		return err
	}
	hash, err := executableHash(r.FFmpegPath)
	if err != nil || hash != r.FFmpegSHA256 {
		return fmt.Errorf("ACCESS_DENIED: audio executable changed")
	}
	node, err := buildDSP(ctx, r.Plan.GainDB, r.DSPChain, r.DSPPlugins)
	if err != nil {
		return err
	}
	defer node.Close()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	decodeArgs := []string{"-hide_banner", "-nostdin", "-v", "error", "-protocol_whitelist", "http,tcp", "-format_whitelist", inputFormats, "-i", r.InputURL, "-ss", strconv.FormatFloat(float64(r.Plan.PositionMs)/1000, 'f', 3, 64), "-map", "0:a:0", "-vn", "-ac", "2", "-ar", "48000", "-f", "f32le", "-acodec", "pcm_f32le", "pipe:1"}
	decoder := exec.CommandContext(ctx, r.FFmpegPath, decodeArgs...)
	encoder := exec.CommandContext(ctx, r.FFmpegPath, "-hide_banner", "-nostdin", "-v", "error", "-f", "f32le", "-ar", "48000", "-ac", "2", "-i", "pipe:0", "-vn", "-codec:a", "libmp3lame", "-b:a", "128k", "-f", "mp3", "-write_xing", "0", "-id3v2_version", "0", "pipe:1")
	decoder.Stderr = io.Discard
	encoder.Stderr = io.Discard
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
		return fmt.Errorf("TEMPORARY_FAILURE: encoder failed to start")
	}
	if err = decoder.Start(); err != nil {
		cancel()
		encodeInput.Close()
		encoder.Wait()
		return fmt.Errorf("TEMPORARY_FAILURE: decoder failed to start")
	}
	err = transferPCM(ctx, decoded, encodeInput, node)
	encodeInput.Close()
	if err != nil {
		cancel()
	}
	decodeErr := decoder.Wait()
	if decodeErr != nil {
		cancel()
	}
	encodeErr := encoder.Wait()
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("CANCELED: audio processing stopped")
	}
	if err != nil {
		return err
	}
	if ctx.Err() != nil {
		return fmt.Errorf("CANCELED: audio processing stopped")
	}
	if decodeErr != nil || encodeErr != nil {
		return fmt.Errorf("TEMPORARY_FAILURE: audio processing failed")
	}
	return nil
}

func transferPCM(ctx context.Context, input io.Reader, output io.Writer, node audio.Node) error {
	buffer := make([]byte, 4096*2*4)
	samples := make([]float32, 4096*2)
	var frames int64
	var sequence uint64
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := io.ReadFull(input, buffer)
		if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
			return fmt.Errorf("TEMPORARY_FAILURE: decoder read failed")
		}
		if n%8 != 0 {
			return fmt.Errorf("PROTOCOL_MISMATCH: unaligned PCM payload")
		}
		if n == 0 {
			break
		}
		for i := range n / 4 {
			samples[i] = math.Float32frombits(binary.LittleEndian.Uint32(buffer[i*4:]))
		}
		block := audio.Block{Format: audio.Canonical, Samples: samples[:n/4], Frames: n / 8, Sequence: sequence, PositionFrames: frames, End: err != nil}
		if processErr := node.Process(&block); processErr != nil {
			return processErr
		}
		for i, sample := range block.Samples {
			binary.LittleEndian.PutUint32(buffer[i*4:], math.Float32bits(sample))
		}
		if _, writeErr := output.Write(buffer[:n]); writeErr != nil {
			return fmt.Errorf("TEMPORARY_FAILURE: encoder write failed")
		}
		frames += int64(block.Frames)
		sequence++
		if err != nil {
			break
		}
	}
	if frames == 0 {
		return fmt.Errorf("UNSUPPORTED: no decodable audio frames")
	}
	return node.Drain()
}
