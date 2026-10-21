package dspworker_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"math"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/dsp/gain"
	"github.com/fnMrRice/MusicFS-server/src/audio"
	"github.com/fnMrRice/MusicFS-server/src/audio/dspworker"
)

func frame(kind uint32, data []byte) []byte {
	header := [12]byte{'M', 'F', 'D', '1'}
	binary.LittleEndian.PutUint32(header[4:], kind)
	binary.LittleEndian.PutUint32(header[8:], uint32(len(data)))
	return append(header[:], data...)
}

func serveFrames(input []byte) ([]byte, error) {
	var output bytes.Buffer
	err := dspworker.Serve(context.Background(), bytes.NewReader(input), &output, "test.gain", "0.1.0", func(params map[string]float64) (audio.Node, error) {
		return dsp.NewGain(params["gainDB"])
	})
	return output.Bytes(), err
}

func TestDSPServeRequiresReleaseVersion(t *testing.T) {
	for _, version := range []string{"", " ", "1.2", "1.2.3-01"} {
		called := false
		err := dspworker.Serve(t.Context(), bytes.NewReader(nil), io.Discard, "test.gain", version, func(map[string]float64) (audio.Node, error) {
			called = true
			return dsp.NewGain(0)
		})
		if err == nil || called {
			t.Fatalf("invalid release reached DSP initialization: %q", version)
		}
	}
}

func TestBinaryFramesBoundPCMAndRejectOversize(t *testing.T) {
	hello, _ := json.Marshal(map[string]any{"protocolVersion": 1, "name": "test.gain", "nodeVersion": audio.NodeVersion, "format": audio.Canonical, "params": map[string]float64{"gainDB": 0}})
	pcm := make([]byte, 24+4096*8)
	binary.LittleEndian.PutUint32(pcm[16:], 4096)
	out, err := serveFrames(append(frame(1, hello), frame(2, pcm)...))
	if err != nil {
		t.Fatal(err)
	}
	r := bytes.NewReader(out)
	var header [12]byte
	if _, err = io.ReadFull(r, header[:]); err != nil {
		t.Fatal(err)
	}
	if _, err = io.CopyN(io.Discard, r, int64(binary.LittleEndian.Uint32(header[8:]))); err != nil {
		t.Fatal(err)
	}
	if _, err = io.ReadFull(r, header[:]); err != nil {
		t.Fatal(err)
	}
	if string(header[:4]) != "MFD1" || binary.LittleEndian.Uint32(header[4:]) != 2 || binary.LittleEndian.Uint32(header[8:]) != uint32(len(pcm)) {
		t.Fatal("PCM payload is not bounded binary data")
	}
	decoded, _ := io.ReadAll(r)
	if !bytes.Equal(pcm, decoded) {
		t.Fatal("binary PCM round trip changed the block")
	}
	oversize := frame(2, nil)
	binary.LittleEndian.PutUint32(oversize[8:], 64*1024+1)
	if _, err := serveFrames(append(frame(1, hello), oversize...)); err == nil {
		t.Fatal("oversized DSP frame accepted")
	}
	w, err := dspworker.New(t.Context(), helperConfig(t, "gain"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	block := &audio.Block{Format: audio.Canonical, Frames: 1, Samples: []float32{float32(math.NaN()), 0}}
	if err := w.Process(block); err == nil {
		t.Fatal("non-finite PCM accepted")
	}
}
