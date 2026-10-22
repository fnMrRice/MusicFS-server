package audiocodecs_test

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	audioffmpeg "github.com/fnMrRice/MusicFS-server/plugins/decoder/ffmpeg"
	ncmdecoder "github.com/fnMrRice/MusicFS-server/plugins/decoder/ncm"
	"github.com/fnMrRice/MusicFS-server/sdk/audioformat"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugincontrol"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func ncmFixture(t *testing.T, name string) ([]byte, audioformat.Input) {
	t.Helper()
	plain, err := os.ReadFile(filepath.Join("..", "..", "testdata", "audio", name))
	if err != nil {
		t.Fatal(err)
	}
	// 故意提供不可信格式字段，链必须使用实际字节识别后续解码器。
	encrypted, err := testkit.BuildNCM(plain, map[string]any{"musicName": "链式解码测试", "album": "CC0", "artist": [][]any{{"MusicFS", 1}}, "format": "untrusted"})
	if err != nil {
		t.Fatal(err)
	}
	return plain, audioformat.Input{Reader: bytes.NewReader(encrypted), Size: int64(len(encrypted))}
}

func ncmProcessor(t *testing.T) (*ncmdecoder.Processor, *plugincontrol.Gate) {
	t.Helper()
	processor, err := ncmdecoder.NewProcessor(ncmdecoder.Config{Builtin: true, MaxConcurrent: 1, WorkDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	gate := plugincontrol.NewGate(true)
	processor.SetGate(gate)
	return processor, gate
}

func audioProcessor(t *testing.T) (*audioffmpeg.Processor, *plugincontrol.Gate, string, string) {
	t.Helper()
	ffmpeg, ffprobe := toolPath(t, "MUSICFS_FFMPEG", "ffmpeg"), toolPath(t, "MUSICFS_FFPROBE", "ffprobe")
	workDir := t.TempDir()
	processor, err := audioffmpeg.NewProcessor(t.Context(), audioffmpeg.Config{Builtin: true, FFmpegPath: ffmpeg, FFprobePath: ffprobe, WorkDir: workDir, MaxConcurrent: 1})
	if err != nil {
		t.Fatal(err)
	}
	gate := plugincontrol.NewGate(true)
	processor.SetGate(gate)
	return processor, gate, workDir, ffprobe
}

func toolPath(t *testing.T, variable, executable string) string {
	t.Helper()
	path := os.Getenv(variable)
	if path == "" {
		var err error
		path, err = exec.LookPath(executable)
		if err != nil {
			t.Skipf("set %s or add %s to PATH for actual codec validation", variable, executable)
		}
	}
	path, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func decodeRequest(format string) audioformat.Request {
	return audioformat.Request{TaskID: "codec-chain-test", AssetID: "codec-source-asset", OutputFormat: format, MaxOutputBytes: 16 << 20}
}

func readOutput(t *testing.T, input audioformat.Input) []byte {
	t.Helper()
	data, err := io.ReadAll(io.NewSectionReader(input.Reader, 0, input.Size))
	if err != nil || int64(len(data)) != input.Size {
		t.Fatalf("output read failed: size=%d bytes=%d error=%v", input.Size, len(data), err)
	}
	return data
}

func assertPCMFormat(t *testing.T, ffprobe string, data []byte, codec string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "output.wav")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(t.Context(), ffprobe, "-v", "error", "-select_streams", "a:0", "-show_entries", "stream=codec_name,sample_fmt,sample_rate,channels", "-of", "json", path)
	raw, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("ffprobe failed: %v %s", err, raw)
	}
	var result struct {
		Streams []struct {
			Codec      string `json:"codec_name"`
			Sample     string `json:"sample_fmt"`
			SampleRate string `json:"sample_rate"`
			Channels   int    `json:"channels"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(raw, &result); err != nil || len(result.Streams) != 1 {
		t.Fatalf("invalid audio probe: %v %s", err, raw)
	}
	wantSample := "flt"
	if codec == audioffmpeg.DecodeSigned16 {
		wantSample = "s16"
	}
	stream := result.Streams[0]
	if stream.Codec != codec || stream.Sample != wantSample || stream.SampleRate != "48000" || stream.Channels < 1 {
		t.Fatalf("decoder output does not match requested PCM: %+v", stream)
	}
}

func assertNoCodecFiles(t *testing.T, directory string) {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(directory, "musicfs-codec-*.tmp"))
	if err != nil || len(files) != 0 {
		t.Fatalf("codec output resources remain: %v %v", files, err)
	}
}
