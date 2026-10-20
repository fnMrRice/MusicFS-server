package plugins_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/plugins/decoder/ffmpeg"
)

func audioTools(t *testing.T) (string, string) {
	t.Helper()
	ffmpeg := os.Getenv("MUSICFS_FFMPEG")
	ffprobe := os.Getenv("MUSICFS_FFPROBE")
	if ffmpeg == "" || ffprobe == "" {
		t.Skip("set MUSICFS_FFMPEG and MUSICFS_FFPROBE to run actual audio tool integration")
	}
	return ffmpeg, ffprobe
}

func buildAudioWorker(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "audio-worker")
	if runtime.GOOS == "windows" {
		path += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", path, "./plugins/decoder/ffmpeg/cmd/musicfs-plugin-audio-ffmpeg")
	cmd.Dir = filepath.Join("..", "..", "..")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	return path
}

func toneWave(seconds int) []byte {
	size := seconds * 48000 * 2
	b := make([]byte, 44+size)
	copy(b, "RIFF")
	binary.LittleEndian.PutUint32(b[4:], uint32(len(b)-8))
	copy(b[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(b[16:], 16)
	binary.LittleEndian.PutUint16(b[20:], 1)
	binary.LittleEndian.PutUint16(b[22:], 1)
	binary.LittleEndian.PutUint32(b[24:], 48000)
	binary.LittleEndian.PutUint32(b[28:], 96000)
	binary.LittleEndian.PutUint16(b[32:], 2)
	binary.LittleEndian.PutUint16(b[34:], 16)
	copy(b[36:], "data")
	binary.LittleEndian.PutUint32(b[40:], uint32(size))
	for i := range size / 2 {
		amplitude := 0.0
		if i >= 48000 && i < 96000 {
			amplitude = .2 * math.Sin(2*math.Pi*440*float64(i)/48000)
		}
		binary.LittleEndian.PutUint16(b[44+i*2:], uint16(int16(amplitude*32767)))
	}
	return b
}

func newProcessor(t *testing.T) *audioffmpeg.Processor {
	t.Helper()
	ffmpeg, ffprobe := audioTools(t)
	worker := buildAudioWorker(t)
	b, err := os.ReadFile(worker)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	p, err := audioffmpeg.NewProcessor(context.Background(), audioffmpeg.Config{FFmpegPath: ffmpeg, FFprobePath: ffprobe, WorkerPath: worker, WorkerSHA256: fmt.Sprintf("%x", sum), WorkDir: t.TempDir(), MaxConcurrent: 1})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func decodePCM(t *testing.T, ffmpeg string, mp3 []byte) []float32 {
	t.Helper()
	cmd := exec.Command(ffmpeg, "-v", "error", "-i", "pipe:0", "-f", "f32le", "-ac", "1", "-ar", "48000", "pipe:1")
	cmd.Stdin = bytes.NewReader(mp3)
	var out, stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("decode output %v %s", err, stderr.String())
	}
	data := out.Bytes()
	result := make([]float32, len(data)/4)
	for i := range result {
		result[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[i*4:]))
	}
	return result
}

func rms(samples []float32) float64 {
	sum := 0.0
	for _, s := range samples {
		sum += float64(s * s)
	}
	return math.Sqrt(sum / float64(len(samples)))
}

func TestActualAudioWorkerGainSeekAndMetadata(t *testing.T) {
	p := newProcessor(t)
	ffmpeg, ffprobe := audioTools(t)
	input := toneWave(4)
	originalHash := sha256.Sum256(input)
	metadata, err := audioffmpeg.ProbeMetadata(context.Background(), ffprobe, bytes.NewReader(input), int64(len(input)))
	if err != nil {
		t.Fatal(err)
	}
	if metadata.DurationMs == nil || *metadata.DurationMs != 4000 || metadata.Codec == nil || *metadata.Codec != "pcm_s16le" {
		t.Fatalf("unexpected metadata %+v", metadata)
	}
	var zero, gain, seek bytes.Buffer
	for i, item := range []struct {
		db       float64
		position int64
		out      *bytes.Buffer
	}{{0, 0, &zero}, {-6.020599913, 0, &gain}, {0, 1500, &seek}} {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		err := p.Process(ctx, audioffmpeg.Plan{TaskID: fmt.Sprintf("task-%d", i), AssetID: "asset", Profile: "mp3-128", GainDB: item.db, PositionMs: item.position}, bytes.NewReader(input), int64(len(input)), item.out)
		cancel()
		if err != nil {
			t.Fatal(err)
		}
	}
	pcm0 := decodePCM(t, ffmpeg, zero.Bytes())
	pcmGain := decodePCM(t, ffmpeg, gain.Bytes())
	pcmSeek := decodePCM(t, ffmpeg, seek.Bytes())
	ratio := rms(pcmGain[60000:80000]) / rms(pcm0[60000:80000])
	if math.Abs(ratio-.5) > .03 {
		t.Fatalf("gain ratio=%f", ratio)
	}
	first := -1
	for i, s := range pcmSeek {
		if math.Abs(float64(s)) > .02 {
			first = i
			break
		}
	}
	if first < 0 || first > 4800 {
		t.Fatalf("seek audio onset outside 100ms tolerance: %d", first)
	}
	last := 0
	for i, s := range pcmSeek {
		if math.Abs(float64(s)) > .02 {
			last = i
		}
	}
	errorMs := math.Abs(float64(last)/48 - 500)
	if errorMs > 100 {
		t.Fatalf("seek end marker error=%.3fms", errorMs)
	}
	if sha256.Sum256(input) != originalHash {
		t.Fatal("input source changed")
	}
	t.Logf("Measured gain ratio=%.6f, seek onset=%.3fms, seek marker error=%.3fms, ffmpeg=%s", ratio, float64(first)/48, errorMs, p.Capabilities.Version)
}

type disconnectWriter struct{ writes atomic.Int32 }

func (w *disconnectWriter) Write(p []byte) (int, error) { w.writes.Add(1); return 0, io.ErrClosedPipe }
func TestActualAudioConsumerDisconnectStopsWorker(t *testing.T) {
	p := newProcessor(t)
	input := toneWave(30)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	start := time.Now()
	writer := &disconnectWriter{}
	err := p.Process(ctx, audioffmpeg.Plan{TaskID: "cancel", AssetID: "a", Profile: "mp3-128"}, bytes.NewReader(input), int64(len(input)), writer)
	if err == nil || writer.writes.Load() == 0 {
		t.Fatalf("disconnect did not fail processing: %v", err)
	}
	if time.Since(start) > 10*time.Second {
		t.Fatal("consumer disconnect did not stop worker promptly")
	}
	t.Logf("Consumer disconnect cleanup=%s", time.Since(start))
}

type heldWriter struct {
	first   chan struct{}
	release chan struct{}
	output  bytes.Buffer
	once    atomic.Bool
}

func (w *heldWriter) Write(b []byte) (int, error) {
	if w.once.CompareAndSwap(false, true) {
		close(w.first)
		<-w.release
	}
	return w.output.Write(b)
}

func TestActualAudioProducesOutputBeforeCompletion(t *testing.T) {
	p := newProcessor(t)
	input := toneWave(30)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	writer := &heldWriter{first: make(chan struct{}), release: make(chan struct{})}
	done := make(chan error, 1)
	start := time.Now()
	go func() {
		done <- p.Process(ctx, audioffmpeg.Plan{TaskID: "streaming", AssetID: "a", Profile: "mp3-128"}, bytes.NewReader(input), int64(len(input)), writer)
	}()
	select {
	case <-writer.first:
		t.Logf("First streamed MP3 chunk=%s", time.Since(start))
	case err := <-done:
		t.Fatalf("completed without streaming chunk: %v", err)
	case <-ctx.Done():
		t.Fatal("first chunk timeout")
	}
	select {
	case err := <-done:
		t.Fatalf("processing finished while first output chunk blocked: %v", err)
	default:
	}
	close(writer.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if writer.output.Len() < 100000 {
		t.Fatalf("unexpected output size %d", writer.output.Len())
	}
}
