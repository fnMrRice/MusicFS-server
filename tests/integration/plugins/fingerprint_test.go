package plugins_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"math/bits"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/audio"
	"github.com/fnMrRice/MusicFS-server/src/identification/fingerprint"
	"github.com/fnMrRice/MusicFS-server/src/plugins/audioffmpeg"
)

func melodyWave(seconds int) []byte {
	b := toneWave(seconds)
	for i := 0; i < seconds*48000; i++ {
		t := float64(i) / 48000
		step := int(t * 4)
		freq := 220 * math.Pow(2, float64((step*7+step/8)%24)/12)
		env := .7 + .3*math.Sin(2*math.Pi*3*t)
		sample := .18*env*math.Sin(2*math.Pi*freq*t) + .09*math.Sin(2*math.Pi*(110+float64(step%7)*31)*t)
		binary.LittleEndian.PutUint16(b[44+i*2:], uint16(int16(sample*32767)))
	}
	return b
}

func fingerprintExtractor(t *testing.T, p *audioffmpeg.Processor, n fingerprint.Normalizer) *fingerprint.Extractor {
	t.Helper()
	path := os.Getenv("MUSICFS_FPCALC")
	if path == "" {
		t.Skip("set MUSICFS_FPCALC to run the pinned Chromaprint integration")
	}
	if !p.Capabilities.Normalization {
		t.Skip("FFmpeg lacks the s16le normalization muxer or encoder")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	e, err := fingerprint.New(context.Background(), fingerprint.Config{FpcalcPath: path, FpcalcSHA256: fmt.Sprintf("%x", sum), FFmpegSHA256: p.Capabilities.FFmpegSHA256, FFmpegVersion: p.Capabilities.Version}, n)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestFingerprintActualWorkerRangesAndQuality(t *testing.T) {
	p := newProcessor(t)
	e := fingerprintExtractor(t, p, p)
	wave := melodyWave(32)
	request := fingerprint.Request{TaskID: "fp-full", AssetID: "asset", DurationMs: 120000}
	var direct bytes.Buffer
	if _, err := p.Normalize(context.Background(), request, bytes.NewReader(wave), int64(len(wave)), &direct); err != nil {
		t.Fatalf("direct normalize: %v", err)
	}
	d, err := e.Extract(context.Background(), request, bytes.NewReader(wave), int64(len(wave)))
	if err != nil {
		t.Fatal(err)
	}
	if d.PCM.SampleCount != 32*11025 || d.Quality.Status != "available" || len(d.Values) < 100 {
		t.Fatalf("invalid detail %+v", d.Quality)
	}
	if d.Identity.Fpcalc.SHA256 == "" || d.Identity.Decoder.SHA256 != p.Capabilities.FFmpegSHA256 || d.Identity.ConfigRevision != e.Revision() {
		t.Fatal("tool identity missing")
	}
	if b, err := d.Encode(); err != nil || len(b) > fingerprint.MaxDetailBytes {
		t.Fatal("detail cannot be published")
	}
	altered := d
	altered.PCM.SampleCount++
	altered.RequestedDurationMs = 10000
	if _, err := altered.Encode(); err == nil {
		t.Fatal("detail accepted a false coverage range")
	}
	altered = d
	altered.Identity.Decoder.SHA256 = "bad"
	if _, err := altered.Encode(); err == nil {
		t.Fatal("detail accepted a replaced decoder identity")
	}
	altered = d
	altered.Values = make([]uint32, 1001)
	if _, err := altered.Encode(); err == nil {
		t.Fatal("detail accepted excess fingerprint items")
	}
	var full bytes.Buffer
	_, err = p.Normalize(context.Background(), request, bytes.NewReader(wave), int64(len(wave)), &full)
	if err != nil {
		t.Fatal(err)
	}
	for _, span := range [][2]int64{{1, 10000}, {7, 10007}, {30, 10030}, {25000, 120000}} {
		request.TaskID = "fp-range"
		request.StartMs, request.DurationMs = span[0], span[1]
		var got bytes.Buffer
		info, err := p.Normalize(context.Background(), request, bytes.NewReader(wave), int64(len(wave)), &got)
		if err != nil {
			t.Fatal(err)
		}
		first, count, _ := request.Samples()
		last := min(first+count, int64(full.Len()/2))
		if info.SampleCount != last-first || !bytes.Equal(got.Bytes(), full.Bytes()[first*2:last*2]) {
			t.Fatal("normalization range differs from exact full-PCM slice")
		}
	}
	short := toneWave(2)
	request.StartMs, request.DurationMs = 0, 10000
	d, err = e.Extract(context.Background(), request, bytes.NewReader(short), int64(len(short)))
	if err != nil || d.Quality.Status != "short" || d.PCM.SampleCount != 22050 {
		t.Fatalf("short EOF became full coverage: %v %+v", err, d.Quality)
	}
	silence := toneWave(12)
	clear(silence[44:])
	d, err = e.Extract(context.Background(), request, bytes.NewReader(silence), int64(len(silence)))
	if err != nil || d.Quality.Status != "lowInformation" {
		t.Fatalf("silence accepted: %v %+v", err, d.Quality)
	}
}

func TestFingerprintEncodingInvariantAndNoExternalRead(t *testing.T) {
	p := newProcessor(t)
	e := fingerprintExtractor(t, p, p)
	wave := melodyWave(24)
	r := fingerprint.Request{TaskID: "encoded", AssetID: "asset", DurationMs: 120000}
	original, err := e.Extract(context.Background(), r, bytes.NewReader(wave), int64(len(wave)))
	if err != nil {
		t.Fatal(err)
	}
	ffmpeg, _ := audioTools(t)
	cmd := exec.Command(ffmpeg, "-v", "error", "-i", "pipe:0", "-c:a", "libmp3lame", "-b:a", "64k", "-f", "mp3", "pipe:1")
	cmd.Stdin = bytes.NewReader(wave)
	encoded, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	compressed, err := e.Extract(context.Background(), r, bytes.NewReader(encoded), int64(len(encoded)))
	if err != nil {
		t.Fatal(err)
	}
	if value := bestFingerprintBER(original.Values, compressed.Values, 3, 100); value > .08 {
		t.Fatalf("synthetic 64k encoding diverged: BER=%f", value)
	}
	prefixed := append([]byte(nil), wave[:44]...)
	prefixed = append(prefixed, make([]byte, 3*48000*2)...)
	prefixed = append(prefixed, wave[44:]...)
	binary.LittleEndian.PutUint32(prefixed[4:], uint32(len(prefixed)-8))
	binary.LittleEndian.PutUint32(prefixed[40:], uint32(len(prefixed)-44))
	shifted, err := e.Extract(context.Background(), r, bytes.NewReader(prefixed), int64(len(prefixed)))
	if err != nil {
		t.Fatal(err)
	}
	if value := bestFingerprintBER(original.Values, shifted.Values, 30, 100); value > .08 {
		t.Fatalf("synthetic silence alignment diverged: BER=%f", value)
	}
	flac, err := os.ReadFile(filepath.Join("..", "..", "testdata", "audio", "lossless.flac"))
	if err != nil {
		t.Fatal(err)
	}
	decode := exec.Command(ffmpeg, "-v", "error", "-i", "pipe:0", "-c:a", "pcm_s16le", "-f", "wav", "pipe:1")
	decode.Stdin = bytes.NewReader(flac)
	decoded, err := decode.Output()
	if err != nil {
		t.Fatal(err)
	}
	a, err := e.Extract(context.Background(), r, bytes.NewReader(flac), int64(len(flac)))
	if err != nil {
		t.Fatal(err)
	}
	b, err := e.Extract(context.Background(), r, bytes.NewReader(decoded), int64(len(decoded)))
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(a.Values) != fmt.Sprint(b.Values) {
		t.Fatal("lossless rewrapping changed raw fingerprint")
	}
	var hits atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1); w.WriteHeader(404) }))
	defer server.Close()
	playlist := []byte("#EXTM3U\n#EXT-X-TARGETDURATION:10\n#EXTINF:10,\n" + server.URL + "/remote\n#EXT-X-ENDLIST\n")
	if _, err = e.Extract(context.Background(), r, bytes.NewReader(playlist), int64(len(playlist))); err == nil || hits.Load() != 0 {
		t.Fatal("playlist accepted or remote URL contacted")
	}
}

// The fixture-specific BER check tests robustness, never a production match rule.
func bestFingerprintBER(a, b []uint32, shiftLimit, minItems int) float64 {
	best := math.Inf(1)
	for shift := -shiftLimit; shift <= shiftLimit; shift++ {
		first := max(0, -shift)
		last := min(len(a), len(b)-shift)
		if last-first < minItems {
			continue
		}
		distance := 0
		for i := first; i < last; i++ {
			distance += bits.OnesCount32(a[i] ^ b[i+shift])
		}
		best = min(best, float64(distance)/float64(32*(last-first)))
	}
	return best
}

type waitingNormalizer struct{ entered chan struct{} }

func (n waitingNormalizer) Normalize(ctx context.Context, _ audio.NormalizeRequest, _ io.ReaderAt, _ int64, _ io.Writer) (audio.PCMInfo, error) {
	close(n.entered)
	<-ctx.Done()
	return audio.PCMInfo{}, ctx.Err()
}
func TestFingerprintCancellationAndAdmission(t *testing.T) {
	p := newProcessor(t)
	n := waitingNormalizer{entered: make(chan struct{})}
	e := fingerprintExtractor(t, p, n)
	ctx, cancel := context.WithCancel(context.Background())
	r := fingerprint.Request{TaskID: "cancel", AssetID: "asset", DurationMs: 10000}
	done := make(chan error, 1)
	go func() { _, err := e.Extract(ctx, r, bytes.NewReader([]byte{1}), 1); done <- err }()
	select {
	case <-n.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("normalizer never started")
	}
	if _, err := e.Extract(context.Background(), r, bytes.NewReader([]byte{1}), 1); err == nil {
		t.Fatal("concurrency bound ignored")
	}
	start := time.Now()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("fingerprint child did not exit")
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("unbounded cancellation")
	}
}
