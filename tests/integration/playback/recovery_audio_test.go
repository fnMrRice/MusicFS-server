package playback_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/decoder/ffmpeg"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func recoveryAudioProcessor(t *testing.T) (*audioffmpeg.Processor, string) {
	t.Helper()
	ffmpeg, ffprobe := os.Getenv("MUSICFS_FFMPEG"), os.Getenv("MUSICFS_FFPROBE")
	if ffmpeg == "" || ffprobe == "" {
		t.Skip("set MUSICFS_FFMPEG and MUSICFS_FFPROBE for actual recovery audio validation")
	}
	worker := filepath.Join(t.TempDir(), "audio-worker")
	if runtime.GOOS == "windows" {
		worker += ".exe"
	}
	cmd := exec.Command("go", "build", "-mod=readonly", "-o", worker, "./plugins/decoder/ffmpeg/cmd/musicfs-plugin-audio-ffmpeg")
	cmd.Dir = testkit.RepositoryRoot()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build worker: %v %s", err, out)
	}
	body, err := os.ReadFile(worker)
	if err != nil {
		t.Fatal(err)
	}
	p, err := audioffmpeg.NewProcessor(context.Background(), audioffmpeg.Config{FFmpegPath: ffmpeg, FFprobePath: ffprobe, WorkerPath: worker, WorkerSHA256: fmt.Sprintf("%x", sha256.Sum256(body)), WorkDir: t.TempDir(), MaxConcurrent: 1})
	if err != nil {
		t.Fatal(err)
	}
	return p, ffmpeg
}
func TestCrossSourceRecoveryActualAudioWithLeadingSilence(t *testing.T) {
	p, ffmpeg := recoveryAudioProcessor(t)
	f := recoverySetup(t, p, 0)
	e := f.evidence(t, 1)
	d := f.create(t, "real-audio-create")
	before, err := os.ReadFile(filepath.Join(filepath.Dir(f.path), f.tracks[1].Path))
	if err != nil {
		t.Fatal(err)
	}
	result, err := f.player.RecoverSource(context.Background(), f.who, d.SessionID, recoveryRequest(d, f.tracks[1].ID, &e), "real-aligned-recovery")
	if err != nil || result.Decision != "continue" || result.Playback == nil {
		t.Fatalf("actual recovery: %+v %v", result, err)
	}
	stream, err := f.player.OpenStream(context.Background(), f.who, d.SessionID, result.Playback.AttemptID, false)
	if err != nil {
		t.Fatal(err)
	}
	var mp3 bytes.Buffer
	err = stream.Process(&mp3)
	stream.Close()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(ffmpeg, "-v", "error", "-i", "pipe:0", "-f", "f32le", "-ac", "1", "-ar", "48000", "pipe:1")
	cmd.Stdin = bytes.NewReader(mp3.Bytes())
	var decoded, stderr bytes.Buffer
	cmd.Stdout = &decoded
	cmd.Stderr = &stderr
	if err = cmd.Run(); err != nil {
		t.Fatalf("decode processed recovery: %v %s", err, stderr.String())
	}
	raw := decoded.Bytes()
	samples := make([]float64, len(raw)/4)
	for i := range samples {
		samples[i] = float64(math.Float32frombits(binary.LittleEndian.Uint32(raw[i*4:])))
	}
	if math.Abs(float64(len(samples))/48-2000) > 100 {
		t.Fatalf("aligned output duration %.3fms", float64(len(samples))/48)
	}
	energy := func(hz float64) float64 {
		var re, im float64
		for i := 12000; i < 60000; i++ {
			angle := 2 * math.Pi * hz * float64(i) / 48000
			re += samples[i] * math.Cos(angle)
			im += samples[i] * math.Sin(angle)
		}
		return re*re + im*im
	}
	if len(samples) < 60000 || energy(660) < 100*energy(440) || energy(660) < 1 {
		t.Fatal("aligned recovery did not contain the expected 660Hz marker")
	}
	onset := -1
	for i, sample := range samples {
		if math.Abs(sample) > .005 {
			onset = i
			break
		}
	}
	if onset < 0 || onset > 4800 {
		t.Fatalf("recovery marker exceeded 100ms codec tolerance: %d", onset)
	}
	after, err := os.ReadFile(filepath.Join(filepath.Dir(f.path), f.tracks[1].Path))
	if err != nil || sha256.Sum256(before) != sha256.Sum256(after) {
		t.Fatal("recovery changed the original source")
	}
	t.Logf("Actual aligned recovery: source=4000ms, target=5000ms, decoded=%.3fms, 660Hz onset=%.3fms", float64(len(samples))/48, float64(onset)/48)
}
