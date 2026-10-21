package dspworker_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"math"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/audio"
	"github.com/fnMrRice/MusicFS-server/src/audio/dspworker"
)

func helperConfig(t *testing.T, mode string) dspworker.Config {
	t.Helper()
	path := fixtureBinary
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	hash := sha256.New()
	if _, err = io.Copy(hash, file); err != nil {
		t.Fatal(err)
	}
	return dspworker.Config{Name: "test.gain", Executable: path, SHA256: hex.EncodeToString(hash.Sum(nil)), Args: []string{mode}, TimeoutMs: 2000}
}

func TestExternalDSPRoundTrip(t *testing.T) {
	p, err := dspworker.New(context.Background(), helperConfig(t, "gain"), map[string]float64{"gainDB": -6})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if p.PluginVersion() != "0.1.0" {
		t.Fatalf("worker release was not verified: %q", p.PluginVersion())
	}
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
	if err := p.Close(); err != nil {
		t.Fatal("worker was not reaped", err)
	}
}

func TestExternalDSPFailuresReapWorker(t *testing.T) {
	for _, mode := range []string{"hang", "crash", "retime"} {
		t.Run(mode, func(t *testing.T) {
			cfg := helperConfig(t, mode)
			cfg.TimeoutMs = 1000
			p, err := dspworker.New(context.Background(), cfg, map[string]float64{"gainDB": 0})
			if err != nil {
				t.Fatal(err)
			}
			defer p.Close()
			// The public configuration controls operation deadlines.
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
	p, err := dspworker.New(ctx, helperConfig(t, "hang"), map[string]float64{"gainDB": 0})
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
	if _, err := dspworker.New(context.Background(), config, nil); err == nil {
		t.Fatal("incorrect executable checksum accepted")
	}
	config = helperConfig(t, "gain")
	config.Name = "different.node"
	if _, err := dspworker.New(context.Background(), config, nil); err == nil {
		t.Fatal("incorrect DSP identity accepted")
	}
}

func TestExternalDSPRejectsUnsupportedContracts(t *testing.T) {
	for _, mode := range []string{"stateful", "delayed", "version", "missing-version", "blank-version", "invalid-version"} {
		t.Run(mode, func(t *testing.T) {
			p, err := dspworker.New(context.Background(), helperConfig(t, mode), nil)
			if p != nil {
				p.Close()
			}
			if err == nil {
				t.Fatal("unsupported DSP contract accepted")
			}
		})
	}
}
