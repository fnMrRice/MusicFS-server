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
	for _, id := range []string{"mp3-128", "opus-5.1", "opus-7.1"} {
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
		encoded, _ := json.Marshal(profile)
		wire := testkit.DecodeJSON(t, encoded).(map[string]any)
		wire["channels"] = float64(2)
		wire["layout"] = "stereo"
		if id != "mp3-128" && testkit.ContractSchema(t, "plugin", "AudioProfile").Validate(wire) == nil {
			t.Fatal("plugin contract accepted surround metadata with stereo encoding")
		}
		timelineRaw, _ := json.Marshal(timeline.Profile{OutputProfile: id, ProcessingRevision: "dsp-v1:" + strings.Repeat("a", 64), DecoderSHA256: strings.Repeat("b", 64)})
		testkit.ValidateHTTP(t, "TimelineProfile", timelineRaw)
	}
}
