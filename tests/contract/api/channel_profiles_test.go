package api_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/decoder/ffmpeg"
	"github.com/fnMrRice/MusicFS-server/src/playback"
	"github.com/fnMrRice/MusicFS-server/src/playback/timeline"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestChannelOutputProfilesBindCodecLayoutAndEncoding(t *testing.T) {
	for _, id := range []string{"mp3-32", "mp3-112", "mp3-128", "mp3-192", "mp3-320", "opus-5.1", "opus-7.1", "opus-5.1-500", "opus-5.1-1536000", "opus-7.1-2048000"} {
		profile, ok := audioffmpeg.ProfileByID(id)
		if !ok {
			t.Fatal("missing supported channel profile", id)
		}
		testkit.ValidatePlugin(t, "AudioProfile", profile)
		testkit.ValidatePlugin(t, "AudioPlan", audioffmpeg.Plan{TaskID: "channel-task", AssetID: "asset", Profile: id})
		testkit.ValidatePlugin(t, "AudioPrepared", map[string]any{"profile": id, "positionOriginMs": 0, "seekMode": "timeRestart"})
		output := playback.OutputProfile{ID: id, MIME: profile.MIME, Codec: &profile.Codec, Container: &profile.Container, Transport: "htmlAudio",
			SampleRate: profile.SampleRate, Channels: profile.Channels, Layout: profile.Layout, BitrateBps: profile.BitrateBps}
		raw, err := json.Marshal(output)
		if err != nil {
			t.Fatal(err)
		}
		testkit.ValidateHTTP(t, "OutputProfile", raw)
		value := testkit.DecodeJSON(t, raw).(map[string]any)
		value["channels"] = float64(3)
		if testkit.ContractSchema(t, "http", "OutputProfile").Validate(value) == nil {
			t.Fatal("HTTP accepted a mismatched output channel count")
		}
		value["channels"] = float64(profile.Channels)
		value["bitrateBps"] = float64(profile.BitrateBps + 1)
		if strings.Count(id, "-") == 2 {
			value["bitrateBps"] = float64(0)
		}
		if testkit.ContractSchema(t, "http", "OutputProfile").Validate(value) == nil {
			t.Fatal("HTTP accepted a mismatched processed output bitrate")
		}
		encoded, _ := json.Marshal(profile)
		wire := testkit.DecodeJSON(t, encoded).(map[string]any)
		wire["channels"] = float64(2)
		wire["layout"] = "stereo"
		if profile.Codec == "opus" && testkit.ContractSchema(t, "plugin", "AudioProfile").Validate(wire) == nil {
			t.Fatal("plugin contract accepted surround metadata with stereo encoding")
		}
		timelineRaw, _ := json.Marshal(timeline.Profile{OutputProfile: id, ProcessingRevision: "dsp-v1:" + strings.Repeat("a", 64), DecoderSHA256: strings.Repeat("b", 64)})
		testkit.ValidateHTTP(t, "TimelineProfile", timelineRaw)
	}
}

func TestOutputProfileContractsRejectInvalidBitrateIdentities(t *testing.T) {
	for _, id := range []string{"mp3-129", "mp3-032", "opus-5.1-499", "opus-5.1-1536001", "opus-7.1-2048001", "opus-5.1-256000", "opus-7.1-384000", "opus-7.1-0320000"} {
		if timeline.ValidOutputProfile(id) {
			t.Fatal("timeline accepted an invalid bitrate identity", id)
		}
		profile := map[string]any{"outputProfile": id, "processingRevision": "gain-v1", "decoderSha256": strings.Repeat("b", 64), "gainDb": float64(0)}
		if testkit.ContractSchema(t, "http", "TimelineProfile").Validate(profile) == nil {
			t.Fatal("timeline contract accepted an invalid bitrate identity", id)
		}
		plan := map[string]any{"profile": id, "taskId": "task", "assetId": "asset", "positionMs": float64(0), "gainDB": float64(0)}
		if testkit.ContractSchema(t, "plugin", "AudioPlan").Validate(plan) == nil {
			t.Fatal("audio plan contract accepted an invalid bitrate identity", id)
		}
	}
}
