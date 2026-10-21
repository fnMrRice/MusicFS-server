package plugins_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/decoder/ffmpeg"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestAudioNormalizeWireContract(t *testing.T) {
	plan := audio.NormalizeRequest{TaskID: "normalize-1", AssetID: "asset-1", StartMs: 4000, DurationMs: 30000}
	request := audioffmpeg.NormalizeRequest{Plan: plan, FFmpegPath: "C:/tools/ffmpeg.exe", FFmpegSHA256: strings.Repeat("a", 64), InputURL: "http://127.0.0.1:12345/" + strings.Repeat("b", 48) + "/input", OutputURL: "http://127.0.0.1:12345/" + strings.Repeat("b", 48) + "/output"}
	info := audio.PCMInfo{Format: audio.FingerprintPCM, NormalizationRevision: audio.NormalizationRevision, StartSample: 44100, SampleCount: 22050, DecoderVersion: "test decoder", DecoderSHA256: request.FFmpegSHA256}
	testkit.ValidatePlugin(t, "AudioNormalize", request)
	testkit.ValidatePlugin(t, "NormalizedPCMInfo", info)
	raw, _ := json.Marshal(request)
	testkit.ValidatePlugin(t, "", pluginproto.Envelope{JSONRPC: "2.0", ID: "h-1", Method: "audio.normalize", Params: raw})
	testkit.ValidatePlugin(t, "", map[string]any{"jsonrpc": "2.0", "id": "h-1", "result": info})
	for _, mutate := range []func(map[string]any){
		func(v map[string]any) { v["inputUrl"] = "https://external.invalid/source" },
		func(v map[string]any) { v["pcm"] = "AAAA" },
		func(v map[string]any) { v["plan"].(map[string]any)["gainDB"] = -6 },
		func(v map[string]any) { v["plan"].(map[string]any)["durationMs"] = 9999 },
		func(v map[string]any) { delete(v, "ffmpegSHA256") },
	} {
		var value map[string]any
		_ = json.Unmarshal(raw, &value)
		mutate(value)
		if err := testkit.ContractSchema(t, "plugin", "AudioNormalize").Validate(value); err == nil {
			t.Fatal("invalid normalization request accepted")
		}
	}
}
