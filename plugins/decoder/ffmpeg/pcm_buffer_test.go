package audioffmpeg

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/dsp/builtin"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
)

func TestPCMBufferSizeControlsBlocksWithoutChangingStatefulSeek(t *testing.T) {
	input := testPCM(20000)
	var reference bytes.Buffer
	if err := transferPCM(t.Context(), bytes.NewReader(input), &reference, &historyNode{}); err != nil {
		t.Fatal(err)
	}
	for _, size := range []int{64, 257, 4096, 16384} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			node := &historyNode{}
			var output bytes.Buffer
			const discard = 4321
			if err := transferPCMFormatsWindow(t.Context(), bytes.NewReader(input), &output, node, audio.Canonical, audio.Canonical, size, pcmWindow{DiscardFrames: discard}); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(output.Bytes(), reference.Bytes()[discard*8:]) || len(node.blocks) != (20000+size-1)/size {
				t.Fatal("buffer selection changed DSP history or did not control block splitting")
			}
			for i, block := range node.blocks {
				if block.Frames != min(size, 20000-i*size) || block.PositionFrames != int64(i*size) || block.Sequence != uint64(i) {
					t.Fatal("wrong block size or timing", block.Frames, block.PositionFrames, block.Sequence)
				}
			}
		})
	}
}

func TestPCMWriterUsesNegotiatedOutputChannelCount(t *testing.T) {
	registry := audio.NewRegistry()
	if err := builtin.Register(registry); err != nil {
		t.Fatal(err)
	}
	for _, channels := range []int{1, 6, 8} {
		t.Run(fmt.Sprint(channels), func(t *testing.T) {
			node, err := audio.NewChain(registry, []audio.NodeSpec{{Name: "channelmix", Version: 1, Params: map[string]float64{"outputChannels": float64(channels)}}})
			if err != nil {
				t.Fatal(err)
			}
			defer node.Close()
			format, err := node.ConfigureIO(audio.Canonical)
			if err != nil {
				t.Fatal(err)
			}
			input := testPCM(19000)
			var result bytes.Buffer
			if err = transferPCMFormatsWindow(context.Background(), bytes.NewReader(input), &result, node, audio.Canonical, format.Format, 16384, pcmWindow{DiscardFrames: 37}); err != nil {
				t.Fatal(err)
			}
			if result.Len() != (19000-37)*channels*4 {
				t.Fatalf("wrong output size: %d", result.Len())
			}
			if channels > 2 {
				for frame := 0; frame < 19000-37; frame++ {
					for channel := range channels {
						got := math.Float32frombits(binary.LittleEndian.Uint32(result.Bytes()[(frame*channels+channel)*4:]))
						want := float32(0)
						if channel < 2 {
							want = math.Float32frombits(binary.LittleEndian.Uint32(input[((frame+37)*2+channel)*4:]))
						}
						if got != want {
							t.Fatal("channel conversion writer corrupted speaker samples", frame, channel)
						}
					}
				}
			}
		})
	}
}
