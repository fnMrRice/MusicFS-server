package audioffmpeg

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
)

// 各源声道占据独立的 200ms 音段，间隔和中部采样避免有损编码边界影响。
func speakerBursts(channels int) []float32 {
	const segment = 9600
	samples := make([]float32, (channels+2)*segment*channels)
	for channel := range channels {
		frequency := 500.0
		if channel == 3 && channels > 2 {
			frequency = 80
		}
		for frame := range segment {
			at := ((channel+1)*segment + frame) * channels
			samples[at+channel] = float32(0.25 * math.Sin(2*math.Pi*frequency*float64(frame)/48000))
		}
	}
	return samples
}

func runWavePipeline(t *testing.T, request StartRequest, wave []byte) []byte {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "speakers.wav", time.Time{}, bytes.NewReader(wave))
	}))
	defer server.Close()
	request.InputURL = server.URL
	var output bytes.Buffer
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	if err := Pipeline(ctx, request, &output); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func decodedOutputPCM(t *testing.T, executable string, encoded []byte) (audio.Format, []float32) {
	t.Helper()
	wave := runPipelineTool(t, executable, encoded, "-v", "error", "-i", "pipe:0", "-c:a", "pcm_f32le", "-f", "wav", "pipe:1")
	format, reader, err := readDecodedWAV(bytes.NewReader(wave))
	if err != nil {
		t.Fatal(err)
	}
	pcm, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	result := make([]float32, len(pcm)/4)
	for i := range result {
		result[i] = math.Float32frombits(binary.LittleEndian.Uint32(pcm[i*4:]))
	}
	return format, result
}

func TestPipelinePreservesSurroundSpeakerRouting(t *testing.T) {
	request := pipelineFixture(t)
	for _, tc := range []struct {
		name     string
		channels int
		mask     uint32
		profile  string
		routing  []int
		gain     []float64
	}{
		{"native 7.1", 8, 0x63f, "opus-7.1", []int{0, 1, 2, 3, 4, 5, 6, 7}, nil},
		{"native 5.1", 6, 0x3f, "opus-5.1", []int{0, 1, 2, 3, 4, 5}, nil},
		{"5.1 upmix", 6, 0x3f, "opus-7.1", []int{0, 1, 2, 3, 4, 5}, nil},
		{"side surround upmix", 6, 0x60f, "opus-7.1", []int{0, 1, 2, 3, 6, 7}, nil},
		{"7.1 downmix", 8, 0x63f, "opus-5.1", []int{0, 1, 2, 3, 4, 5, 4, 5}, []float64{1, 1, 1, 1, 0.5, 0.5, 0.5, 0.5}},
		{"stereo upmix", 2, 3, "opus-7.1", []int{0, 1}, nil},
		{"mono upmix", 1, 4, "opus-5.1", []int{2}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request.Plan.Profile = tc.profile
			profile, _ := ProfileByID(tc.profile)
			encoded := runWavePipeline(t, request, testWAV(tc.channels, tc.mask, speakerBursts(tc.channels)))
			format, samples := decodedOutputPCM(t, request.FFmpegPath, encoded)
			if format.Layout != profile.Layout || format.Channels != profile.Channels {
				t.Fatalf("wrong encoded channel layout: %+v", format)
			}
			for source, destination := range tc.routing {
				for channel := range format.Channels {
					rms := speakerRMS(samples, format.Channels, source, channel)
					want := 0.25 / math.Sqrt2
					if tc.gain != nil {
						want *= tc.gain[source]
					}
					if channel == destination {
						if math.Abs(rms-want) > want*0.15 {
							t.Fatalf("source=%d output=%d RMS=%g want=%g", source, channel, rms, want)
						}
					} else if rms > 0.003 {
						t.Fatalf("unexpected speaker leakage: source=%d output=%d RMS=%g", source, channel, rms)
					}
				}
			}
		})
	}
}

func speakerRMS(samples []float32, channels, source, output int) float64 {
	start, end := (source+1)*9600+2400, (source+2)*9600-2400
	if end*channels > len(samples) {
		return 0
	}
	sum := 0.0
	for frame := start; frame < end; frame++ {
		sample := float64(samples[frame*channels+output])
		sum += sample * sample
	}
	return math.Sqrt(sum / float64(end-start))
}

func TestPipelineStereoDownmixOmitsLFEAndUsesBothSurroundPairs(t *testing.T) {
	request := pipelineFixture(t)
	encoded := runWavePipeline(t, request, testWAV(8, 0x63f, speakerBursts(8)))
	format, samples := decodedOutputPCM(t, request.FFmpegPath, encoded)
	if format != audio.Canonical {
		t.Fatalf("wrong stereo output: %+v", format)
	}
	w := math.Sqrt(0.5)
	d := 1 + 3*w
	routes := [][2]float64{{1 / d, 0}, {0, 1 / d}, {w / d, w / d}, {0, 0}, {w / d, 0}, {0, w / d}, {w / d, 0}, {0, w / d}}
	for source, outputs := range routes {
		for channel, coefficient := range outputs {
			actual := speakerRMS(samples, 2, source, channel)
			want := 0.25 / math.Sqrt2 * coefficient
			if math.Abs(actual-want) > max(0.002, want*0.15) {
				t.Fatalf("source=%d output=%d RMS=%g want=%g", source, channel, actual, want)
			}
		}
	}
}

func TestPipelineSurroundSeekReconstructsState(t *testing.T) {
	request := pipelineFixture(t)
	request.DSPChain = []audio.NodeSpec{{Name: "equalizer", Version: 1, Params: map[string]float64{"gainDB": 4}}, {Name: "compressor", Version: 1}, {Name: "reverb", Version: 1, Params: map[string]float64{"wet": 0.6}}}
	for _, profileID := range []string{"opus-5.1", "opus-7.1"} {
		t.Run(profileID, func(t *testing.T) {
			request.Plan.Profile, request.Plan.PositionMs = profileID, 733
			profile, _ := ProfileByID(profileID)
			format, err := audio.FormatForLayout(profile.Layout)
			if err != nil {
				t.Fatal(err)
			}
			mask := uint32(0x3f)
			if format.Channels == 8 {
				mask = 0x63f
			}
			source := speakerBursts(format.Channels)
			actual := runWavePipeline(t, request, testWAV(format.Channels, mask, source))
			node, err := buildDSPFormat(t.Context(), 0, request.DSPChain, nil, format)
			if err != nil {
				t.Fatal(err)
			}
			defer node.Close()
			for offset := 0; offset < len(source); {
				size := min(777*format.Channels, len(source)-offset)
				block := audio.Block{Format: format, Samples: source[offset : offset+size], Frames: size / format.Channels, PositionFrames: int64(offset / format.Channels)}
				if err := node.Process(&block); err != nil {
					t.Fatal(err)
				}
				offset += size
			}
			source = source[request.Plan.PositionMs*48*int64(format.Channels):]
			var referencePCM bytes.Buffer
			for _, sample := range source {
				binary.Write(&referencePCM, binary.LittleEndian, math.Float32bits(sample))
			}
			reference := runPipelineTool(t, request.FFmpegPath, referencePCM.Bytes(), encoderArguments(profile)...)
			_, got := decodedOutputPCM(t, request.FFmpegPath, actual)
			_, want := decodedOutputPCM(t, request.FFmpegPath, reference)
			if len(got) != len(want) {
				t.Fatalf("seek sample count got=%d want=%d", len(got), len(want))
			}
			for i := range got {
				if got[i] != want[i] {
					t.Fatalf("surround seek differs from full-history reference at sample %d", i)
				}
			}
		})
	}
}

func TestPipelineRejectsUnknownMultichannelSpeakerMask(t *testing.T) {
	request := pipelineFixture(t)
	wave := testWAV(6, 0, speakerBursts(6))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "unknown.wav", time.Time{}, bytes.NewReader(wave))
	}))
	defer server.Close()
	request.InputURL = server.URL
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	err := Pipeline(ctx, request, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "decoded channel layout is unknown") {
		t.Fatalf("unknown source layout was guessed or accepted: %v", err)
	}
}
