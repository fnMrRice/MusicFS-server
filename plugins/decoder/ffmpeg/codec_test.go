package audioffmpeg

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/audioformat"
)

func TestSignedPCMDecodePreservesSamplesAndLayout(t *testing.T) {
	format := wavFormatChunk(6, 0x60f)
	binary.LittleEndian.PutUint32(format[8:], 48000*6*2)
	binary.LittleEndian.PutUint16(format[12:], 6*2)
	binary.LittleEndian.PutUint16(format[14:], 16)
	binary.LittleEndian.PutUint16(format[18:], 16)
	format[24] = 1
	var payload bytes.Buffer
	want := []int16{-32768, -16384, 0, 16384, 32767, 12345}
	for _, sample := range want {
		binary.Write(&payload, binary.LittleEndian, sample)
	}
	chunks := append(wavChunk("fmt ", format), wavChunk("data", payload.Bytes())...)
	var wave bytes.Buffer
	wave.WriteString("RIFF")
	binary.Write(&wave, binary.LittleEndian, uint32(4+len(chunks)))
	wave.WriteString("WAVE")
	wave.Write(chunks)
	gotFormat, pcm, err := readDecodedWAVOutput(bytes.NewReader(wave.Bytes()), DecodeSigned16)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(pcm)
	if err != nil || gotFormat.Layout != "5.1(side)" || len(data) != len(want)*4 {
		t.Fatal(gotFormat, len(data), err)
	}
	for index, sample := range want {
		got := math.Float32frombits(binary.LittleEndian.Uint32(data[index*4:]))
		if got != float32(sample)/32768 {
			t.Fatal("PCM conversion changed the decoded sample", index, got)
		}
	}
	if _, _, err := readDecodedWAV(bytes.NewReader(wave.Bytes())); err == nil {
		t.Fatal("worker output did not match the requested float format")
	}
}

func TestDecoderOutputIdentityAndUnknownFormat(t *testing.T) {
	p := &Processor{identity: ProcessingIdentity{PipelineRevision: "dsp-v1:" + strings.Repeat("a", 64)}}
	if p.ProcessingIdentityForDecodeOutput("") != p.ProcessingIdentityForDecodeOutput(DecodeFloat32) {
		t.Fatal("default decode identity changed")
	}
	if p.ProcessingIdentityForDecodeOutput(DecodeSigned16) == p.ProcessingIdentityForDecodeOutput(DecodeFloat32) {
		t.Fatal("different quantization formats reused one processing identity")
	}
	if _, err := DecodeOutput("script"); err == nil {
		t.Fatal("unknown decoder output accepted")
	}
}

func TestCodecWorkerRejectsInvalidRequestsBeforeStartingTools(t *testing.T) {
	w := &audioWorker{ready: true, taskID: "task", capabilities: Capabilities{DecodeOutputs: []string{DecodeFloat32}, TranscodeFormats: []string{"mp3"}}}
	base := CodecRequest{TaskID: "task", AssetID: "asset", OutputFormat: DecodeFloat32, MaxOutputBytes: 1024,
		InputURL: "http://127.0.0.1:12345/input", OutputURL: "http://127.0.0.1:12345/output"}
	for _, tc := range []struct {
		name, method string
		edit         func(*CodecRequest)
	}{
		{"undeclared PCM", "audio.decode", func(r *CodecRequest) { r.OutputFormat = DecodeSigned16 }},
		{"encoded output for decode", "audio.decode", func(r *CodecRequest) { r.OutputFormat = "mp3" }},
		{"PCM output for transcode", "audio.transcode", func(r *CodecRequest) {}},
		{"oversized output budget", "audio.decode", func(r *CodecRequest) { r.MaxOutputBytes = MaxDecodeOutputBytes + 1 }},
		{"wrong task", "audio.decode", func(r *CodecRequest) { r.TaskID = "another-task" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := base
			tc.edit(&request)
			raw, _ := json.Marshal(request)
			if _, err := w.handle(t.Context(), tc.method, raw); err == nil || strings.Contains(err.Error(), "executable changed") {
				t.Fatal("invalid codec request reached the tool", err)
			}
		})
	}
	raw, _ := json.Marshal(base)
	raw = append(raw[:len(raw)-1], []byte(`,"extra":"command"}`)...)
	if _, err := w.handle(t.Context(), "audio.decode", raw); err == nil {
		t.Fatal("unknown codec request field accepted")
	}
}

func TestActualTranscodeFormatsAndOutputBudget(t *testing.T) {
	request := pipelineFixture(t)
	ffprobe := os.Getenv("MUSICFS_FFPROBE")
	if ffprobe == "" {
		ffprobe = filepath.Join(filepath.Dir(request.FFmpegPath), "ffprobe"+filepath.Ext(request.FFmpegPath))
	}
	root := t.TempDir()
	p, err := NewProcessor(t.Context(), Config{Builtin: true, WorkDir: root, FFmpegPath: request.FFmpegPath, FFprobePath: ffprobe})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "tests", "testdata", "audio", "markers.wav"))
	if err != nil {
		t.Fatal(err)
	}
	input := audioformat.Input{Reader: bytes.NewReader(data), Size: int64(len(data)), Format: "wav", MIME: "audio/wav"}
	for _, format := range []string{"mp3", "flac", "opus", "wav"} {
		t.Run(format, func(t *testing.T) {
			output, err := p.Transcode(t.Context(), audioformat.Request{TaskID: "transcode-" + format, AssetID: "asset", OutputFormat: format}, input)
			if err != nil {
				t.Fatal(err)
			}
			defer output.Close()
			payload, err := io.ReadAll(io.NewSectionReader(output.Reader, 0, output.Size))
			if err != nil || len(payload) == 0 || output.NeedsDecoding {
				t.Fatal("invalid transcode result", err)
			}
			probe := runPipelineTool(t, ffprobe, payload, "-v", "error", "-show_entries", "stream=codec_name", "-of", "json", "pipe:0")
			want := format
			if format == "wav" {
				want = DecodeSigned16
			}
			if !bytes.Contains(probe, []byte(`"codec_name": "`+want+`"`)) {
				t.Fatal("wrong transcode codec", string(probe))
			}
			if err := output.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
	_, err = p.Decode(context.Background(), audioformat.Request{TaskID: "budget", AssetID: "asset", OutputFormat: DecodeFloat32, MaxOutputBytes: 64}, input)
	if err == nil {
		t.Fatal("unbounded decoder output accepted")
	}
	files, err := filepath.Glob(filepath.Join(root, "musicfs-codec-*.tmp"))
	if err != nil || len(files) != 0 {
		t.Fatal("codec output file leaked", files, err)
	}
}

func TestNativeDecodeWithoutToolLeavesSourceOwnedByCaller(t *testing.T) {
	data := []byte("ID3native fixture")
	var p *Processor
	output, err := p.Decode(t.Context(), audioformat.Request{OutputFormat: "native"}, audioformat.Input{Reader: bytes.NewReader(data), Size: int64(len(data))})
	if err != nil || output.Close != nil || output.NeedsDecoding || output.Format != "mp3" {
		t.Fatal(output, err)
	}
	if _, err := p.Decode(t.Context(), audioformat.Request{OutputFormat: DecodeFloat32}, output.Input); err == nil {
		t.Fatal("PCM decode ran without a configured tool")
	}
}
