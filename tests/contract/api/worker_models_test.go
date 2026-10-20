package api_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/decoder/ffmpeg"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/audio"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestHostAndAudioWireDTOsMatchContract(t *testing.T) {
	testkit.ValidatePlugin(t, "EntriesRequest", pluginproto.EntriesRequest{GrantID: "grant-1", RootID: "root", Limit: 200})
	testkit.ValidatePlugin(t, "EntriesPage", pluginproto.EntriesPage{Items: []pluginproto.Entry{{RootID: "root", RelativePath: "a.wav", FileIdentity: "file", ObservedVersion: "v1", MIME: "audio/wav", SizeBytes: 44}}, RootIdentity: "root-v1", Issues: []string{}})
	testkit.ValidatePlugin(t, "StateGet", pluginproto.StateRequest{Key: "track-1"})
	testkit.ValidatePlugin(t, "StateCAS", pluginproto.StateRequest{Key: "track-1", Revision: 1, Value: []byte(`{"id":"track-1"}`)})
	testkit.ValidatePlugin(t, "StateResult", pluginproto.StateValue{Revision: 0})
	testkit.ValidatePlugin(t, "ResourceRequest", pluginproto.Resource{RootID: "root", RelativePath: "a.wav", ObservedVersion: "v1"})
	capabilities := audioffmpeg.Capabilities{Version: "test FFmpeg", Build: "test build", FFmpegSHA256: strings.Repeat("a", 64), FFprobeSHA256: strings.Repeat("b", 64), PCM: audio.Canonical,
		Profiles: []audioffmpeg.Profile{{ID: "mp3-128", MIME: "audio/mpeg", Codec: "mp3", Container: "mp3", SampleRate: 48000, Channels: 2, BitrateBps: 128000, SeekMode: "timeRestart"}}}
	testkit.ValidatePlugin(t, "AudioCapabilities", capabilities)
	request := audioffmpeg.StartRequest{Plan: audioffmpeg.Plan{TaskID: "task-1", AssetID: "asset-1", Profile: "mp3-128", GainDB: -6, PositionMs: 2000},
		FFmpegPath: "C:/tools/ffmpeg.exe", FFmpegSHA256: strings.Repeat("a", 64), InputURL: "http://127.0.0.1:12345/" + strings.Repeat("c", 48) + "/input", OutputURL: "http://127.0.0.1:12345/" + strings.Repeat("c", 48) + "/output"}
	testkit.ValidatePlugin(t, "AudioStart", request)
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	testkit.ValidatePlugin(t, "", pluginproto.Envelope{JSONRPC: "2.0", ID: "h-1", Method: "audio.start", Params: raw})
	request.InputURL = "https://untrusted.invalid/song"
	raw, _ = json.Marshal(request)
	if err := testkit.ContractSchema(t, "plugin", "AudioStart").Validate(testkit.DecodeJSON(t, raw)); err == nil {
		t.Fatal("schema accepted a non-loopback input channel")
	}
	request.InputURL = request.OutputURL
	request.Plan.GainDB = 60
	raw, _ = json.Marshal(request)
	if err := testkit.ContractSchema(t, "plugin", "AudioStart").Validate(testkit.DecodeJSON(t, raw)); err == nil {
		t.Fatal("schema accepted an unsupported DSP gain")
	}
}
