package audioffmpeg

import (
	"bytes"
	"log/slog"
	"math"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginlog"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
)

func TestDisabledChannelPluginPreservesActualSpeakerSignals(t *testing.T) {
	request := pipelineFixture(t)
	request.DisableEffects, request.DisableChannelMix = true, true
	request.Plan.GainDB = -40
	request.DSPChain = []audio.NodeSpec{{Name: "channelmix", Version: 1, Params: map[string]float64{"outputChannels": 8}}}
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: pluginlog.LevelTrace})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	for _, tc := range []struct {
		channels int
		mask     uint32
		profile  string
	}{{1, 4, "mp3-mono-128"}, {2, 3, "mp3-128"}, {6, 0x3f, "opus-5.1"}, {8, 0x63f, "opus-7.1"}} {
		t.Run(tc.profile, func(t *testing.T) {
			request.Plan.Profile = tc.profile
			encoded := runWavePipeline(t, request, testWAV(tc.channels, tc.mask, speakerBursts(tc.channels)))
			format, samples := decodedOutputPCM(t, request.FFmpegPath, encoded)
			if format.Channels != tc.channels {
				t.Fatalf("disabled converter changed channel count: %+v", format)
			}
			for source := range tc.channels {
				for channel := range tc.channels {
					rms := speakerRMS(samples, tc.channels, source, channel)
					want := 0.0
					if channel == source {
						want = .25 / math.Sqrt2
					}
					if math.Abs(rms-want) > .03 {
						t.Fatalf("speaker lost or rerouted: source=%d output=%d RMS=%g", source, channel, rms)
					}
				}
			}
		})
	}
	if strings.Contains(logs.String(), "musicfs.plugin.decoder.channelmix") {
		t.Fatal("disabled channel plugin was instantiated", logs.String())
	}
}

func TestDisabledChannelPluginUsesEncoderCompatibilityDownmix(t *testing.T) {
	request := pipelineFixture(t)
	request.DisableEffects, request.DisableChannelMix = true, true
	encoded := runWavePipeline(t, request, testWAV(8, 0x63f, speakerBursts(8)))
	format, samples := decodedOutputPCM(t, request.FFmpegPath, encoded)
	if format.Channels != 2 || speakerRMS(samples, 2, 6, 0) < .03 || speakerRMS(samples, 2, 7, 1) < .03 {
		t.Fatal("necessary MP3 compatibility downmix lost surround content")
	}
}
