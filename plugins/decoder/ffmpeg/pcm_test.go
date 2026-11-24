package audioffmpeg

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
)

type historyNode struct {
	previous [2]float32
	blocks   []audio.Block
	drained  bool
	process  func()
}

func (*historyNode) DescribeIO(format audio.Format) (audio.Output, error) {
	return audio.PCMOutput(format)
}
func (n *historyNode) ConfigureIO(format audio.Format) (audio.Output, error) {
	return n.DescribeIO(format)
}
func (*historyNode) Reset() error       { return nil }
func (n *historyNode) Drain() error     { n.drained = true; return nil }
func (*historyNode) Close() error       { return nil }
func (*historyNode) DelayFrames() int64 { return 0 }
func (n *historyNode) Process(block *audio.Block) error {
	if n.process != nil {
		n.process()
	}
	n.blocks = append(n.blocks, *block)
	for i := range block.Samples {
		channel := i % 2
		n.previous[channel] = block.Samples[i]*0.1 + n.previous[channel]*0.9
		block.Samples[i] = n.previous[channel]
	}
	return nil
}

type boundedReader struct {
	input io.Reader
	limit int
}

func (r boundedReader) Read(p []byte) (int, error) {
	return r.input.Read(p[:min(len(p), r.limit)])
}

func testPCM(frames int) []byte {
	data := make([]byte, frames*8)
	for i := 0; i < frames*2; i++ {
		value := float32(math.Sin(float64(i) * 0.007))
		binary.LittleEndian.PutUint32(data[i*4:], math.Float32bits(value))
	}
	return data
}

func TestPCMSeekReplaysHistoryBeforeExactFrameCut(t *testing.T) {
	input := testPCM(10000)
	var full bytes.Buffer
	if err := transferPCM(context.Background(), bytes.NewReader(input), &full, &historyNode{}); err != nil {
		t.Fatal(err)
	}
	for _, skip := range []int64{0, 1, 4095, 4096, 4097, 9999} {
		for _, chunk := range []int{1, 7, 4096, 32768} {
			t.Run(fmt.Sprintf("skip_%d_chunk_%d", skip, chunk), func(t *testing.T) {
				node := &historyNode{}
				var result bytes.Buffer
				reader := boundedReader{bytes.NewReader(input), chunk}
				if err := transferPCMWindow(context.Background(), reader, &result, node, pcmWindow{DiscardFrames: skip}); err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(result.Bytes(), full.Bytes()[skip*8:]) {
					t.Fatal("seek output differs from continuously processed audio")
				}
				if !node.drained || len(node.blocks) != 3 {
					t.Fatalf("history was not fully processed: drained=%v blocks=%d", node.drained, len(node.blocks))
				}
				for i, block := range node.blocks {
					if block.PositionFrames != int64(i*4096) || block.Sequence != uint64(i) || block.Discontinuity {
						t.Fatalf("unexpected history timing: %+v", block)
					}
				}
			})
		}
	}
}

func TestPCMFastSeekPreservesSourcePosition(t *testing.T) {
	node := &historyNode{}
	if err := transferPCMWindow(context.Background(), bytes.NewReader(testPCM(4200)), io.Discard, node, pcmWindow{StartFrame: 48000}); err != nil {
		t.Fatal(err)
	}
	if len(node.blocks) != 2 || node.blocks[0].PositionFrames != 48000 || node.blocks[1].PositionFrames != 52096 || node.blocks[1].Sequence != 1 {
		t.Fatal("fast seek did not preserve source frame positions")
	}
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

type readerFunc func([]byte) (int, error)

func (f readerFunc) Read(p []byte) (int, error) { return f(p) }

func TestPCMRejectsMissingOutputAndShortWrites(t *testing.T) {
	for _, tc := range []struct {
		name   string
		input  []byte
		window pcmWindow
		writer io.Writer
		want   string
	}{
		{"empty", nil, pcmWindow{}, io.Discard, "no decodable audio"},
		{"unaligned", []byte{1}, pcmWindow{}, io.Discard, "unaligned PCM"},
		{"at end", testPCM(5), pcmWindow{DiscardFrames: 5}, io.Discard, "position has no audio"},
		{"past end", testPCM(5), pcmWindow{DiscardFrames: 6}, io.Discard, "position has no audio"},
		{"negative", testPCM(5), pcmWindow{DiscardFrames: -1}, io.Discard, "invalid PCM playback window"},
		{"short write", testPCM(5), pcmWindow{}, writerFunc(func(p []byte) (int, error) { return len(p) - 1, nil }), "encoder write failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			node := &historyNode{}
			err := transferPCMWindow(context.Background(), bytes.NewReader(tc.input), tc.writer, node, tc.window)
			if err == nil || !strings.Contains(err.Error(), tc.want) || node.drained {
				t.Fatalf("error=%v drained=%v", err, node.drained)
			}
		})
	}
}

func TestPCMCancelStopsBeforeWriting(t *testing.T) {
	for _, stage := range []string{"before read", "during read", "during process"} {
		t.Run(stage, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			node := &historyNode{}
			var input io.Reader = bytes.NewReader(testPCM(8000))
			switch stage {
			case "before read":
				cancel()
			case "during read":
				input = readerFunc(func([]byte) (int, error) { cancel(); return 0, io.ErrClosedPipe })
			case "during process":
				node.process = cancel
			}
			var output bytes.Buffer
			err := transferPCMWindow(ctx, input, &output, node, pcmWindow{DiscardFrames: 4000})
			if !errors.Is(err, context.Canceled) || output.Len() != 0 || node.drained {
				t.Fatalf("cancellation error=%v output=%d drained=%v", err, output.Len(), node.drained)
			}
		})
	}
}

type declaredNode struct {
	historyNode
	stateless bool
}

func (n *declaredNode) Stateless() bool { return n.stateless }

func TestChainStatelessRequiresExplicitDeclaration(t *testing.T) {
	for _, tc := range []struct {
		name string
		node audio.Node
		want bool
	}{
		{"empty", nil, true},
		{"unknown", &historyNode{}, false},
		{"stateful", &declaredNode{stateless: false}, false},
		{"stateless", &declaredNode{stateless: true}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			registry := audio.NewRegistry()
			var specs []audio.NodeSpec
			if tc.node != nil {
				if err := registry.Register("probe", func(map[string]float64) (audio.Node, error) { return tc.node, nil }); err != nil {
					t.Fatal(err)
				}
				specs = []audio.NodeSpec{{Name: "probe", Version: audio.NodeVersion}}
			}
			chain, err := audio.NewChain(registry, specs)
			if err != nil {
				t.Fatal(err)
			}
			defer chain.Close()
			if chain.Stateless() != tc.want {
				t.Fatalf("Stateless=%v want=%v", chain.Stateless(), tc.want)
			}
		})
	}
}
