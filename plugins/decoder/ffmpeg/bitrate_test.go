package audioffmpeg

import (
	"bytes"
	"math"
	"strconv"
	"strings"
	"testing"
)

func TestSourceBitrateSelectsClosestEncodingAndKeepsDeploymentImmutable(t *testing.T) {
	for _, tc := range []struct {
		layout string
		source int64
		id     string
		bps    int
	}{
		{"stereo", 0, "mp3-128", 128000},
		{"stereo", -1, "mp3-128", 128000},
		{"stereo", 12000, "mp3-32", 32000},
		{"stereo", 116162, "mp3-112", 112000},
		{"stereo", 120000, "mp3-128", 128000},
		{"stereo", 192000, "mp3-192", 192000},
		{"stereo", 1000000, "mp3-320", 320000},
		{"stereo", math.MaxInt64, "mp3-320", 320000},
		{"5.1", 0, "opus-5.1", 256000},
		{"5.1", 128000, "opus-5.1-128000", 128000},
		{"5.1", 116162, "opus-5.1-116162", 116162},
		{"5.1", 256000, "opus-5.1", 256000},
		{"5.1", 1, "opus-5.1-500", 500},
		{"5.1", math.MaxInt64, "opus-5.1-1536000", 1536000},
		{"7.1", 0, "opus-7.1", 384000},
		{"7.1", 320000, "opus-7.1-320000", 320000},
		{"7.1", math.MaxInt64, "opus-7.1-2048000", 2048000},
	} {
		t.Run(tc.id+"/"+strconv.FormatInt(tc.source, 10), func(t *testing.T) {
			baseID := "mp3-128"
			if tc.layout != "stereo" {
				baseID = "opus-" + tc.layout
			}
			base, _ := ProfileByID(baseID)
			processor := &Processor{config: Config{OutputLayout: tc.layout}, Capabilities: Capabilities{Profiles: []Profile{base}}}
			view, err := processor.WithSourceBitrate(tc.source)
			if err != nil {
				t.Fatal(err)
			}
			selected, err := view.OutputProfile()
			if err != nil || selected.ID != tc.id || selected.BitrateBps != tc.bps {
				t.Fatalf("unexpected selected profile: %+v %v", selected, err)
			}
			unchanged, _ := processor.OutputProfile()
			if unchanged != base || view.DeploymentIdentity() != processor.DeploymentIdentity() {
				t.Fatal("session bitrate changed deployment settings or identity")
			}
			args := strings.Join(encoderArguments(selected), " ")
			if !strings.Contains(args, "-b:a "+strconv.Itoa(tc.bps)+" ") {
				t.Fatalf("encoder command does not match selected bitrate: %s", args)
			}
		})
	}
}

func TestOutputProfilesRejectInvalidOrAmbiguousBitrates(t *testing.T) {
	for _, id := range []string{"mp3-0", "mp3-8", "mp3-129", "mp3-032", "mp3-321", "opus-5.1-499", "opus-5.1-1536001", "opus-7.1-2048001", "opus-5.1-256000", "opus-7.1-384000", "opus-7.1-0320000", "opus-7.1-9223372036854775808"} {
		if _, ok := ProfileByID(id); ok {
			t.Fatalf("invalid profile accepted: %s", id)
		}
	}
}

func TestOpusBitrateBoundsProduceDecodableAudio(t *testing.T) {
	request := pipelineFixture(t)
	for _, id := range []string{"opus-5.1-500", "opus-5.1-1536000", "opus-7.1-500", "opus-7.1-2048000"} {
		t.Run(id, func(t *testing.T) {
			request.Plan.Profile = id
			var output bytes.Buffer
			if err := Pipeline(t.Context(), request, &output); err != nil {
				t.Fatal("Opus bitrate bound could not be encoded", id, err)
			}
			profile, _ := ProfileByID(id)
			format, samples := decodedOutputPCM(t, request.FFmpegPath, output.Bytes())
			if format.Channels != profile.Channels || len(samples) == 0 {
				t.Fatal("encoded Opus bitrate bound did not retain decodable channels", id, format)
			}
		})
	}
}
