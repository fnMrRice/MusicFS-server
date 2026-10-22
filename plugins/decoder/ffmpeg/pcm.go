package audioffmpeg

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"math"

	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
)

// pcmWindow 用源帧位置标记解码结果，并在处理后裁切需重建历史的帧。
type pcmWindow struct {
	StartFrame    int64
	DiscardFrames int64
}

func transferPCM(ctx context.Context, input io.Reader, output io.Writer, node audio.Node) error {
	return transferPCMWindow(ctx, input, output, node, pcmWindow{})
}

func transferPCMWindow(ctx context.Context, input io.Reader, output io.Writer, node audio.Node, window pcmWindow) error {
	return transferPCMFormatWindow(ctx, input, output, node, audio.Canonical, window)
}

func transferPCMFormatWindow(ctx context.Context, input io.Reader, output io.Writer, node audio.Node, format audio.Format, window pcmWindow) error {
	return transferPCMFormatsWindow(ctx, input, output, node, format, format, 4096, window)
}

func transferPCMFormatsWindow(ctx context.Context, input io.Reader, output io.Writer, node audio.Node, format, outputFormat audio.Format, framesPerBlock int, window pcmWindow) error {
	if err := format.Validate(); err != nil {
		return err
	}
	if err := outputFormat.Validate(); err != nil {
		return err
	}
	framesPerBlock, err := bufferFrames(framesPerBlock)
	if err != nil {
		return err
	}
	if window.StartFrame < 0 || window.DiscardFrames < 0 {
		return fmt.Errorf("UNSUPPORTED: invalid PCM playback window")
	}
	bytesPerFrame := format.Channels * 4
	buffer := make([]byte, framesPerBlock*bytesPerFrame)
	encoded := make([]byte, framesPerBlock*outputFormat.Channels*4)
	samples := make([]float32, framesPerBlock*format.Channels)
	var frames, emitted int64
	var sequence uint64
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, readErr := io.ReadFull(input, buffer)
		if err := ctx.Err(); err != nil {
			return err
		}
		if readErr != nil && readErr != io.EOF && readErr != io.ErrUnexpectedEOF {
			return fmt.Errorf("TEMPORARY_FAILURE: decoder read failed")
		}
		if n%bytesPerFrame != 0 {
			return fmt.Errorf("PROTOCOL_MISMATCH: unaligned PCM payload")
		}
		if n == 0 {
			break
		}
		for i := range n / 4 {
			samples[i] = math.Float32frombits(binary.LittleEndian.Uint32(buffer[i*4:]))
		}
		block := audio.Block{Format: format, Samples: samples[:n/4], Frames: n / bytesPerFrame, Sequence: sequence, PositionFrames: window.StartFrame + frames, End: readErr != nil}
		before := block
		if processErr := node.Process(&block); processErr != nil {
			return processErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := block.Validate(); err != nil {
			return err
		}
		if block.Format != outputFormat {
			return fmt.Errorf("PROTOCOL_MISMATCH: DSP output differs from encoding format")
		}
		if block.Frames != before.Frames || block.Sequence != before.Sequence || block.PositionFrames != before.PositionFrames || block.End != before.End || block.Discontinuity != before.Discontinuity {
			return fmt.Errorf("PROTOCOL_MISMATCH: DSP changed block timing")
		}
		// 每个节点都看到完整 PCM 块；裁切不改变节点历史或块序号。
		discard := min(int64(block.Frames), max(int64(0), window.DiscardFrames-frames))
		outSamples := block.Samples[discard*int64(outputFormat.Channels):]
		for i, sample := range outSamples {
			binary.LittleEndian.PutUint32(encoded[i*4:], math.Float32bits(sample))
		}
		if len(outSamples) > 0 {
			written, writeErr := output.Write(encoded[:len(outSamples)*4])
			if err := ctx.Err(); err != nil {
				return err
			}
			if writeErr != nil || written != len(outSamples)*4 {
				return fmt.Errorf("TEMPORARY_FAILURE: encoder write failed")
			}
			emitted += int64(block.Frames) - discard
		}
		frames += int64(block.Frames)
		sequence++
		if readErr != nil {
			break
		}
	}
	if frames == 0 {
		return fmt.Errorf("UNSUPPORTED: no decodable audio frames")
	}
	if emitted == 0 {
		return fmt.Errorf("UNSUPPORTED: playback position has no audio frames")
	}
	return node.Drain()
}
