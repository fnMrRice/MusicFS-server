package audioffmpeg

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
)

func pipelineFixture(t *testing.T) StartRequest {
	t.Helper()
	ffmpeg := os.Getenv("MUSICFS_FFMPEG")
	if ffmpeg == "" {
		var err error
		ffmpeg, err = exec.LookPath("ffmpeg")
		if err != nil {
			t.Skip("set MUSICFS_FFMPEG or add ffmpeg to PATH for actual pipeline validation")
		}
	}
	ffmpeg, err := filepath.Abs(ffmpeg)
	if err != nil {
		t.Fatal(err)
	}
	hash, err := executableHash(ffmpeg)
	if err != nil {
		t.Fatal(err)
	}
	wave, err := os.ReadFile(filepath.Join("..", "..", "..", "tests", "testdata", "audio", "markers.wav"))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "markers.wav", time.Time{}, bytes.NewReader(wave))
	}))
	t.Cleanup(server.Close)
	return StartRequest{Plan: Plan{Profile: "mp3-128"}, FFmpegPath: ffmpeg, FFmpegSHA256: hash, InputURL: server.URL}
}

func runPipelineTool(t *testing.T, executable string, input []byte, args ...string) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.Stdin = bytes.NewReader(input)
	var output, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("audio tool failed: %v %s", err, stderr.String())
	}
	return output.Bytes()
}

// 参考路径故意使用不同于播放管线的块大小，验证节点历史不依赖分块。
func processReferencePCM(t *testing.T, input []byte, specs []audio.NodeSpec) []byte {
	t.Helper()
	node, err := buildDSP(t.Context(), 0, specs, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer node.Close()
	output := bytes.Clone(input)
	var sequence uint64
	for offset := 0; offset < len(output); {
		size := min(777*8, len(output)-offset)
		samples := make([]float32, size/4)
		for i := range samples {
			samples[i] = math.Float32frombits(binary.LittleEndian.Uint32(output[offset+i*4:]))
		}
		block := audio.Block{Format: audio.Canonical, Samples: samples, Frames: size / 8, Sequence: sequence, PositionFrames: int64(offset / 8), End: offset+size == len(output)}
		if err := node.Process(&block); err != nil {
			t.Fatal(err)
		}
		for i, sample := range block.Samples {
			binary.LittleEndian.PutUint32(output[offset+i*4:], math.Float32bits(sample))
		}
		offset += size
		sequence++
	}
	if err := node.Drain(); err != nil {
		t.Fatal(err)
	}
	return output
}

func TestPipelineSeekMatchesContinuouslyProcessedAudio(t *testing.T) {
	request := pipelineFixture(t)
	pcm := runPipelineTool(t, request.FFmpegPath, nil, "-hide_banner", "-nostdin", "-v", "error", "-i", request.InputURL, "-map", "0:a:0", "-vn", "-ac", "2", "-ar", "48000", "-f", "f32le", "-acodec", "pcm_f32le", "pipe:1")
	eq := audio.NodeSpec{Name: "equalizer", Version: 1, Params: map[string]float64{"frequencyHz": 440, "q": 4, "gainDB": 8}}
	compressor := audio.NodeSpec{Name: "compressor", Version: 1, Params: map[string]float64{"thresholdDB": -24, "attackMs": 20, "releaseMs": 500}}
	reverb := audio.NodeSpec{Name: "reverb", Version: 1, Params: map[string]float64{"wet": 0.65, "roomSize": 0.8}}
	for _, tc := range []struct {
		name  string
		specs []audio.NodeSpec
	}{
		{"gain fast path", nil},
		{"equalizer", []audio.NodeSpec{eq}},
		{"compressor", []audio.NodeSpec{compressor}},
		{"reverb", []audio.NodeSpec{reverb}},
		{"combined", []audio.NodeSpec{eq, compressor, reverb}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request.DSPChain = tc.specs
			request.Plan.PositionMs = 2137
			processed := processReferencePCM(t, pcm, tc.specs)
			processed = processed[request.Plan.PositionMs*48*8:]
			want := runPipelineTool(t, request.FFmpegPath, processed, "-hide_banner", "-nostdin", "-v", "error", "-f", "f32le", "-ar", "48000", "-ac", "2", "-i", "pipe:0", "-vn", "-codec:a", "libmp3lame", "-b:a", "128k", "-f", "mp3", "-write_xing", "0", "-id3v2_version", "0", "pipe:1")
			var got bytes.Buffer
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			if err := Pipeline(ctx, request, &got); err != nil {
				t.Fatal(err)
			}
			if len(want) == 0 || !bytes.Equal(got.Bytes(), want) {
				t.Fatalf("seek output differs from continuous DSP reference: got=%d want=%d", got.Len(), len(want))
			}
		})
	}
}

func TestPipelineStatefulSeekPastEndFails(t *testing.T) {
	request := pipelineFixture(t)
	request.Plan.PositionMs = 10000
	request.DSPChain = []audio.NodeSpec{{Name: "reverb", Version: 1}}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	err := Pipeline(ctx, request, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "position has no audio frames") {
		t.Fatalf("out-of-range seek returned %v", err)
	}
}

func TestPipelineCanceledBeforeStart(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := Pipeline(ctx, StartRequest{}, io.Discard)
	if err == nil || !strings.HasPrefix(err.Error(), "CANCELED:") {
		t.Fatalf("canceled pipeline returned %v", err)
	}
}
