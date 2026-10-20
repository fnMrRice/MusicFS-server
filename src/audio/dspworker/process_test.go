package dspworker

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"io"
	"math"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/plugins/dsp/gain"
	"github.com/fnMrRice/MusicFS-server/src/audio"
)

type helperNode struct {
	audio.Node
	mode string
}

func (h *helperNode) Stateless() bool { return h.mode != "stateful" }
func (h *helperNode) DelayFrames() int64 {
	if h.mode == "delayed" {
		return 1
	}
	return 0
}
func (h *helperNode) Process(block *audio.Block) error {
	switch h.mode {
	case "hang":
		time.Sleep(time.Hour)
	case "crash":
		os.Exit(19)
	case "retime":
		block.PositionFrames++
	}
	return h.Node.Process(block)
}

func TestDSPHelper(t *testing.T) {
	index := slices.Index(os.Args, "--musicfs-dsp-helper")
	if index < 0 {
		return
	}
	mode := os.Args[index+1]
	if mode == "version" {
		if _, _, err := readFrame(os.Stdin); err != nil {
			os.Exit(3)
		}
		writeFrame(os.Stdout, frameHello, marshal(ready{ProtocolVersion: 2, Name: "test.gain", NodeVersion: audio.NodeVersion, Format: audio.Canonical, PreservesTiming: true, Stateless: true}))
		io.Copy(io.Discard, os.Stdin)
		os.Exit(0)
	}
	err := Serve(context.Background(), os.Stdin, os.Stdout, "test.gain", func(params map[string]float64) (audio.Node, error) {
		node, err := dsp.NewGain(params["gainDB"])
		return &helperNode{Node: node, mode: mode}, err
	})
	if err != nil {
		os.Exit(2)
	}
	os.Exit(0)
}

func helperConfig(t *testing.T, mode string) Config {
	t.Helper()
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	hash := sha256.New()
	if _, err = io.Copy(hash, file); err != nil {
		t.Fatal(err)
	}
	return Config{Name: "test.gain", Executable: path, SHA256: hex.EncodeToString(hash.Sum(nil)), Args: []string{"-test.run=^TestDSPHelper$", "--", "--musicfs-dsp-helper", mode}, TimeoutMs: 2000}
}

func TestExternalDSPRoundTrip(t *testing.T) {
	p, err := New(context.Background(), helperConfig(t, "gain"), map[string]float64{"gainDB": -6})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	block := audio.Block{Format: audio.Canonical, Samples: []float32{1, -1, 0.25, -0.25}, Frames: 2, Sequence: 72, PositionFrames: 480000, End: true, Discontinuity: true}
	if err = p.Process(&block); err != nil {
		t.Fatal(err)
	}
	if math.Abs(float64(block.Samples[0])-math.Pow(10, -6.0/20)) > 0.000001 || block.Sequence != 72 || block.PositionFrames != 480000 || !block.End || !block.Discontinuity {
		t.Fatalf("incorrect processed block: %+v", block)
	}
	if err = p.Reset(); err != nil {
		t.Fatal(err)
	}
	if err = p.Drain(); err != nil {
		t.Fatal(err)
	}
	p.Close()
	select {
	case <-p.done:
	default:
		t.Fatal("worker was not reaped")
	}
}

func TestExternalDSPFailuresReapWorker(t *testing.T) {
	for _, mode := range []string{"hang", "crash", "retime"} {
		t.Run(mode, func(t *testing.T) {
			p, err := New(context.Background(), helperConfig(t, mode), map[string]float64{"gainDB": 0})
			if err != nil {
				t.Fatal(err)
			}
			defer p.Close()
			p.timeout = 50 * time.Millisecond
			block := audio.Block{Format: audio.Canonical, Samples: []float32{1, -1}, Frames: 1}
			started := time.Now()
			if err = p.Process(&block); err == nil {
				t.Fatal("faulty DSP worker succeeded")
			}
			p.Close()
			if time.Since(started) > 3*time.Second {
				t.Fatal("DSP failure did not stop promptly")
			}
		})
	}
}

func TestExternalDSPCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p, err := New(ctx, helperConfig(t, "hang"), map[string]float64{"gainDB": 0})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	result := make(chan error, 1)
	go func() {
		result <- p.Process(&audio.Block{Format: audio.Canonical, Samples: []float32{1, -1}, Frames: 1})
	}()
	cancel()
	select {
	case err = <-result:
		if err == nil {
			t.Fatal("cancelled DSP succeeded")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("DSP cancellation did not stop request")
	}
	p.Close()
}

func TestExternalDSPRejectsChangedExecutableAndIdentity(t *testing.T) {
	config := helperConfig(t, "gain")
	config.SHA256 = strings.Repeat("0", 64)
	if _, err := New(context.Background(), config, nil); err == nil {
		t.Fatal("incorrect executable checksum accepted")
	}
	config = helperConfig(t, "gain")
	config.Name = "different.node"
	if _, err := New(context.Background(), config, nil); err == nil {
		t.Fatal("incorrect DSP identity accepted")
	}
}

func TestExternalDSPRejectsUnsupportedContracts(t *testing.T) {
	for _, mode := range []string{"stateful", "delayed", "version"} {
		t.Run(mode, func(t *testing.T) {
			p, err := New(context.Background(), helperConfig(t, mode), nil)
			if p != nil {
				p.Close()
			}
			if err == nil {
				t.Fatal("unsupported DSP contract accepted")
			}
		})
	}
}

func TestBinaryFramesBoundPCMAndRejectOversize(t *testing.T) {
	block := &audio.Block{Format: audio.Canonical, Frames: 4096, Samples: make([]float32, 8192)}
	data, err := encodeBlock(block)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 24+4096*8 {
		t.Fatalf("PCM payload is not bounded binary data: %d", len(data))
	}
	var frame bytes.Buffer
	if err := writeFrame(&frame, frameProcess, data); err != nil {
		t.Fatal(err)
	}
	kind, decoded, err := readFrame(&frame)
	if err != nil || kind != frameProcess || !bytes.Equal(data, decoded) {
		t.Fatalf("binary framing round trip failed: %v", err)
	}
	header := [12]byte{'M', 'F', 'D', '1'}
	binary.LittleEndian.PutUint32(header[8:], maxPayload+1)
	if _, _, err := readFrame(bytes.NewReader(header[:])); err == nil {
		t.Fatal("oversized DSP frame accepted")
	}
	block.Samples[0] = float32(math.NaN())
	if _, err := encodeBlock(block); err == nil {
		t.Fatal("non-finite PCM accepted")
	}
}
